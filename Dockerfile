# syntax=docker/dockerfile:1.7

##
## Build stage
##
FROM golang:1.22-bookworm AS builder

WORKDIR /src

# Leverage Go module cache
COPY go.mod go.sum ./
RUN go mod download

COPY . .

ARG TARGETOS=linux
ARG TARGETARCH=amd64

RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH \
    go build -ldflags="-s -w" -o /bin/medspa-api ./cmd/api

##
## Runtime stage
##
FROM gcr.io/distroless/base-debian12

ENV PORT=8080
EXPOSE 8080

COPY --from=builder /bin/medspa-api /bin/medspa-api

ENTRYPOINT ["/bin/medspa-api"]
