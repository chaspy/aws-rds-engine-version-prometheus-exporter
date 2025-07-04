FROM golang:1.24.4 as builder

WORKDIR /go/src

COPY go.mod go.sum ./
RUN go mod download

COPY ./main.go  ./

ARG CGO_ENABLED=0
ARG GOOS=linux
ARG GOARCH=amd64
RUN go build \
    -o /go/bin/aws-rds-engine-version-prometheus-exporter \
    -ldflags '-s -w'

FROM alpine:3.22.0 as runner

COPY --from=builder /go/bin/aws-rds-engine-version-prometheus-exporter /app/aws-rds-engine-version-prometheus-exporter

RUN adduser -D -S -H exporter

USER exporter

COPY minimum_supported_version.csv /etc/minimum_supported_version.csv

ENTRYPOINT ["/app/aws-rds-engine-version-prometheus-exporter"]
