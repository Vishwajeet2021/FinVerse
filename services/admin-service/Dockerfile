# Build context is the repo root — see docker/docker-compose.yml.
FROM golang:1.26-alpine AS build
WORKDIR /src

COPY shared/go ./shared/go
COPY services/admin-service ./services/admin-service

WORKDIR /src/services/admin-service
RUN --mount=type=cache,target=/root/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 go build -o /out/admin-service ./cmd/server

FROM alpine:3.20
RUN adduser -D -u 10001 finverse
COPY --from=build /out/admin-service /usr/local/bin/admin-service
USER finverse
EXPOSE 8089
ENTRYPOINT ["/usr/local/bin/admin-service"]
