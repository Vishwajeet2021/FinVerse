# Build context is the repo root (see docker/docker-compose.yml) so this
# Dockerfile can reach shared/go for the `replace` directive in go.mod,
# per docs/13-repository-structure.md §2-3.
FROM golang:1.26-alpine AS build
WORKDIR /src

COPY shared/go ./shared/go
COPY services/auth-service ./services/auth-service

WORKDIR /src/services/auth-service
RUN --mount=type=cache,target=/root/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 go build -o /out/auth-service ./cmd/server

FROM alpine:3.20
RUN adduser -D -u 10001 finverse
# Pre-create and own /keys as the finverse user *before* it's declared a
# volume mount point: on first use, Docker seeds a fresh named volume from
# whatever's already at that path in the image, ownership included — without
# this, the volume would come up root-owned and the non-root process below
# couldn't write config.LoadOrGenerateRSAKey's key file into it.
RUN mkdir -p /keys && chown finverse:finverse /keys
COPY --from=build /out/auth-service /usr/local/bin/auth-service
USER finverse
EXPOSE 8081
ENTRYPOINT ["/usr/local/bin/auth-service"]
