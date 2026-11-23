# Build context is the repo root — see docker/docker-compose.yml.
FROM golang:1.26-alpine AS build
WORKDIR /src

COPY shared/go ./shared/go
COPY services/fraud-service ./services/fraud-service

WORKDIR /src/services/fraud-service
RUN --mount=type=cache,target=/root/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 go build -o /out/fraud-service ./cmd/server

FROM alpine:3.20
RUN adduser -D -u 10001 finverse
COPY --from=build /out/fraud-service /usr/local/bin/fraud-service
USER finverse
EXPOSE 8088 9092
ENTRYPOINT ["/usr/local/bin/fraud-service"]
