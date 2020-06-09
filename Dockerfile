##Builder Image
FROM golang:1.13-stretch as builder
ENV GO111MODULE=on
COPY . /micro
WORKDIR /micro
RUN go mod download
RUN CGO_ENABLED=0 GOOS=linux go build -o bin/application

#s Run Image
FROM scratch
COPY --from=builder /micro/bin/application application
EXPOSE 9999
ENTRYPOINT ["./application"]
