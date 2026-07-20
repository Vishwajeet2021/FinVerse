# Build context is the repo root — see docker/docker-compose.yml.
FROM golang:1.26-alpine AS build
WORKDIR /src

COPY shared/go ./shared/go
COPY services/payment-service ./services/payment-service

WORKDIR /src/services/payment-service
RUN --mount=type=cache,target=/root/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 go build -o /out/payment-service ./cmd/server

FROM alpine:3.20
RUN adduser -D -u 10001 finverse
COPY --from=build /out/payment-service /usr/local/bin/payment-service
USER finverse
EXPOSE 8085
ENTRYPOINT ["/usr/local/bin/payment-service"]
