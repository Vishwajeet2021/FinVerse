# Build context is the repo root — see docker/docker-compose.yml.
FROM golang:1.26-alpine AS build
WORKDIR /src

COPY shared/go ./shared/go
COPY services/reporting-service ./services/reporting-service

WORKDIR /src/services/reporting-service
RUN --mount=type=cache,target=/root/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 go build -o /out/reporting-service ./cmd/server

FROM alpine:3.20
RUN adduser -D -u 10001 finverse && mkdir -p /var/lib/finverse/reports && chown finverse /var/lib/finverse/reports
COPY --from=build /out/reporting-service /usr/local/bin/reporting-service
USER finverse
EXPOSE 8090
ENTRYPOINT ["/usr/local/bin/reporting-service"]
