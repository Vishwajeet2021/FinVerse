# Build context is the repo root — see docker/docker-compose.yml.
FROM golang:1.26-alpine AS build
WORKDIR /src

COPY shared/go ./shared/go
COPY services/wallet-service ./services/wallet-service

WORKDIR /src/services/wallet-service
RUN --mount=type=cache,target=/root/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 go build -o /out/wallet-service ./cmd/server

FROM alpine:3.20
RUN adduser -D -u 10001 finverse
COPY --from=build /out/wallet-service /usr/local/bin/wallet-service
USER finverse
EXPOSE 8084
ENTRYPOINT ["/usr/local/bin/wallet-service"]
