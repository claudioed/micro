package main

import (
	"encoding/json"
	"io/ioutil"
	"os"
	"sync"
)

type AppSettings struct {
	Name    string `json:"name"`
	Version string `json:"version"`
	Apps    []App  `json:"apps"`
}

type App struct {
	Name string `json:"name"`
	Url  string `json:"url"`
}

type AppResponse struct {
	Name    string `json:"name"`
	Version string `json:"version"`
	Status  string `json:"status"`
	Dependencies []*AppResponse
}

func (a *AppResponse) status(status string)  {
	a.Status = status
}

func NewSingleAppResponse(name string,version string,status string)*AppResponse  {
	return &AppResponse{
		Name:         name,
		Version:      version,
		Status:       status,
		Dependencies: nil,
	}
}

func NewAppResponse(name string,version string,status string,dep []*AppResponse)*AppResponse  {
	return &AppResponse{
		Name:         name,
		Version:      version,
		Status:       status,
		Dependencies: dep,
	}
}

var conf *AppSettings
var once sync.Once

func GetAppSettings() *AppSettings {
	once.Do(func() {
		conf = readAppConfiguration()
	})
	return conf
}

func readAppConfiguration() *AppSettings {
	log.Info().Msg("Reading file from... " + os.Getenv("APP_CONFIG_PATH"))
	file, _ := ioutil.ReadFile(os.Getenv("APP_CONFIG_PATH"))
	log.Info().Msg("File content " + string(file))
	appSettings := &AppSettings{}
	if err := json.Unmarshal(file, appSettings); err != nil {
		log.Error().Err(err).Msg("failed to decode app configuration")
	}
	log.Info().Msg("App name " + appSettings.Name)
	return appSettings
}



