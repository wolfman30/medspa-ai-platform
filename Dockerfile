# syntax=docker/dockerfile:1.7

##
## Build stage
##
FROM golang:1.23-bookworm AS builder

WORKDIR /src

# Leverage Go module cache
COPY go.mod go.sum ./
RUN go mod download

COPY . .

ARG TARGETOS=linux
ARG TARGETARCH=amd64

RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH \
    go build -ldflags="-s -w" -o /bin/medspa-api ./cmd/api

RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH \
    go build -ldflags="-s -w" -o /bin/conversation-worker ./cmd/conversation-worker

RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH \
    go build -ldflags="-s -w" -o /bin/voice-lambda ./cmd/voice-lambda

##
## Runtime stage
## 
FROM gcr.io/distroless/base-debian12

ENV PORT=8080
EXPOSE 8080

FROM gcr.io/distroless/base-debian12 AS api

ENV PORT=8080
EXPOSE 8080

COPY --from=builder /bin/medspa-api /bin/medspa-api

ENTRYPOINT ["/bin/medspa-api"]

FROM gcr.io/distroless/base-debian12 AS conversation-worker

COPY --from=builder /bin/conversation-worker /bin/conversation-worker

ENTRYPOINT ["/bin/conversation-worker"]

FROM public.ecr.aws/lambda/provided:al2023 AS voice-lambda

COPY --from=builder /bin/voice-lambda /var/task/bootstrap

CMD ["bootstrap"]
