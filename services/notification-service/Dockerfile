# Build context is the repo root — see docker/docker-compose.yml.
FROM golang:1.26-alpine AS build
WORKDIR /src

COPY shared/go ./shared/go
COPY services/notification-service ./services/notification-service

WORKDIR /src/services/notification-service
RUN --mount=type=cache,target=/root/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 go build -o /out/notification-service ./cmd/server

FROM alpine:3.20
RUN adduser -D -u 10001 finverse
COPY --from=build /out/notification-service /usr/local/bin/notification-service
USER finverse
EXPOSE 8086
ENTRYPOINT ["/usr/local/bin/notification-service"]
