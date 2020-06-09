package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"github.com/labstack/echo"
	"github.com/labstack/echo/middleware"
	"github.com/motemen/go-loghttp"
	"github.com/rs/zerolog"
	"io/ioutil"
	"net/http"
	"os"
	"time"
)

var log *zerolog.Logger
var client *http.Client

func init() {
	zerolog.TimeFieldFormat = zerolog.TimeFormatUnix
	output := zerolog.ConsoleWriter{Out: os.Stdout, TimeFormat: time.RFC3339}
	logger := zerolog.New(output).With().Timestamp().Caller().Logger()
	log = &logger
	transport := &loghttp.Transport{
		LogRequest: func(req *http.Request) {
			log.Debug().
				Interface("headers", req.Header).
				Msg("calling " + req.Method + " " + req.URL.String())
		},
		LogResponse: func(res *http.Response) {
			req := res.Request
			log.Debug().
				Str("status", res.Status).
				Interface("headers", res.Header).
				Msg("call " + req.Method + " " + req.URL.String() + " answered")
		},
	}
	client = &http.Client{Transport: transport}
}

func main() {
	start := time.Now()
	e := echo.New()
	e.Logger.SetOutput(ioutil.Discard)
	// Middleware
	e.Use(func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) (err error) {
			req := c.Request()
			res := c.Response()
			start := time.Now()
			log.Info().
				Interface("headers", req.Header).
				Msg(">>> " + req.Method + " " + req.RequestURI)
			if err = next(c); err != nil {
				c.Error(err)
			}
			log.Info().
				Str("latency", time.Now().Sub(start).String()).
				Int("status", res.Status).
				Interface("headers", res.Header()).
				Msg("<<< " + req.Method + " " + req.RequestURI)
			return
		}
	})
	e.Use(middleware.Recover())
	//CORS
	e.Use(middleware.CORSWithConfig(middleware.CORSConfig{
		AllowOrigins: []string{"*"},
		AllowMethods: []string{echo.GET, echo.HEAD, echo.PUT, echo.PATCH, echo.POST, echo.DELETE},
	}))

	// Server
	e.POST("/api/data", Receive)
	e.GET("/health", Health)
	elapsed := time.Now().Sub(start)
	log.Info().Msg("APP initialized in " + elapsed.String())
	e.Logger.Fatal(e.Start(":9999"))
}

func Health(c echo.Context) error {
	return c.JSON(200, &HealthData{Status: "UP"})
}

type HealthData struct {
	Status string `json:"status,omitempty"`
}

func Receive(c echo.Context) error {
	defer c.Request().Body.Close()
	appSettings := GetAppSettings()
	if len(appSettings.Apps) > 0 {
		log.Info().Msg("Number of dependencies " + string(len(appSettings.Apps)))
		result := call(appSettings, c)
		if containsError(result) {
			return c.JSON(http.StatusServiceUnavailable, NewAppResponse(appSettings.Name, appSettings.Version, "503",transform(result)))
		}
		return c.JSON(http.StatusCreated, NewAppResponse(appSettings.Name, appSettings.Version, "201",transform(result)))
	}
	log.Info().Msg("There is no dependencies")
	return c.JSON(http.StatusCreated, NewSingleAppResponse(appSettings.Name, appSettings.Version, "201"))
}

func forwardHeaders(ctx echo.Context, r *http.Request) {
	incomingHeaders := []string{
		"Authorization",
		"app-version",
		"x-request-id",
		"x-b3-traceid",
		"x-b3-spanid",
		"x-b3-parentspanid",
		"x-b3-sampled",
		"x-b3-flags",
		"x-ot-span-context",
	}
	for _, th := range incomingHeaders {
		h := ctx.Request().Header.Get(th)
		if h != "" {
			r.Header.Set(th, h)
		}
	}
}

func transform(res []Result) []*AppResponse  {
	data := make([]*AppResponse,0, len(res))
	for _, app := range res {
		data = append(data,app.Response)
	}
	return data
}

func call(settings *AppSettings, ctx echo.Context) []Result {
	var results = make([]Result,0, len(settings.Apps))
	for _, app := range settings.Apps {
		res, status, err := postFor(ctx, app)
		results = append(results, Result{
			Response: res,
			Status:   status,
			Error:    err,
		})
	}
	return results
}

func containsError(res []Result) bool {
	for _, data := range res {
		if data.hasError() {
			return true
		}
	}
	return false
}

type Result struct {
	Response *AppResponse
	Status   int
	Error    error
}

func (r *Result) hasError() bool {
	return r.Error != nil
}

func postFor(ctx echo.Context, app App) (*AppResponse, int, error) {
	req, _ := http.NewRequest("POST", app.Url, nil)

	ar := &AppResponse{
		Name:         app.Name,
		Version:      "unknown",
		Dependencies: nil,
	}

	forwardHeaders(ctx, req)
	res, err := client.Do(req)
	if err != nil {
		log.Error().Err(err).Msg(fmt.Sprintf("failed to call %s", app.Name))
		ar.status("0")
		return ar, 0, err
	}
	status := res.StatusCode
	if !is2xx(status) {
		return ar, status, errors.New(res.Status)
	}
	body, readErr := ioutil.ReadAll(res.Body)

	if readErr != nil {
		ar.status("0")
		log.Error().Err(err).Msg(fmt.Sprintf("failed to read %s body", app.Name))
		return ar, status, readErr
	}

	appRes := &AppResponse{}

	log.Info().Msg(fmt.Sprintf("App %s response %s", app.Name, string(body)))

	if jsonErr := json.Unmarshal(body, appRes); jsonErr != nil {
		ar.status("0")
		log.Error().Err(err).Msg(fmt.Sprintf("failed to parse %s body", app.Name))
		return ar, status, jsonErr
	}
	return appRes, status, nil
}

func player(ctx echo.Context) (string, int, error) {
	req, _ := http.NewRequest("GET", os.Getenv("PLAYER_SVC"), nil)

	forwardHeaders(ctx, req)
	res, err := client.Do(req)
	if err != nil {
		log.Error().Err(err).Msg("failed to call players")
		return "", 0, err
	}
	status := res.StatusCode
	if !is2xx(status) {
		return "", status, errors.New(res.Status)
	}
	body, readErr := ioutil.ReadAll(res.Body)
	if readErr != nil {
		log.Error().Err(err).Msg("failed to read players response body")
		return "", status, readErr
	}

	var data map[string]string

	if jsonErr := json.Unmarshal(body, &data); jsonErr != nil {
		log.Error().Err(err).Msg("failed to read players response body")
		return "", status, jsonErr
	}
	return data["email"], status, nil
}

func is2xx(status int) bool {
	return status >= 200 && status < 300
}

type Error struct {
	Errors map[string]int `json:"errors,omitempty"`
}
