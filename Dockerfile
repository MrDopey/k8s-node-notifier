FROM golang:1.23-alpine AS build

WORKDIR /controller

COPY . .

RUN apk add -u -t build-tools curl git && \
    CGO_ENABLED=0 GOOS=linux go build -a -installsuffix cgo -o controller cmd/controller/*.go && \
    apk del build-tools && \
    rm -rf /var/cache/apk/*

FROM alpine:latest

WORKDIR /app

COPY --from=build /controller/controller /controller

ENTRYPOINT ["/controller"]
