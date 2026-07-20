# Build context is the repo root — see docker/docker-compose.yml — so this
# Dockerfile can reach shared/go for the `replace` directive in go.mod,
# per docs/13-repository-structure.md §2-3.
FROM golang:1.26-alpine AS build
WORKDIR /src

COPY shared/go ./shared/go
COPY services/account-service ./services/account-service

WORKDIR /src/services/account-service
RUN --mount=type=cache,target=/root/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 go build -o /out/account-service ./cmd/server

FROM alpine:3.20
RUN adduser -D -u 10001 finverse
COPY --from=build /out/account-service /usr/local/bin/account-service
USER finverse
EXPOSE 8083
ENTRYPOINT ["/usr/local/bin/account-service"]
