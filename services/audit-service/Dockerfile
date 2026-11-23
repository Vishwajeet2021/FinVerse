# Build context is the repo root — see docker/docker-compose.yml.
FROM golang:1.26-alpine AS build
WORKDIR /src

COPY shared/go ./shared/go
COPY services/audit-service ./services/audit-service

WORKDIR /src/services/audit-service
RUN --mount=type=cache,target=/root/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 go build -o /out/audit-service ./cmd/server

FROM alpine:3.20
RUN adduser -D -u 10001 finverse
COPY --from=build /out/audit-service /usr/local/bin/audit-service
USER finverse
EXPOSE 8087
ENTRYPOINT ["/usr/local/bin/audit-service"]
