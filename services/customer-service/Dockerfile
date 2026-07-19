# Build context is the repo root (see docker/docker-compose.yml) so this
# Dockerfile can reach shared/go for the `replace` directive in go.mod,
# per docs/13-repository-structure.md §2-3.
FROM golang:1.26-alpine AS build
WORKDIR /src

COPY shared/go ./shared/go
COPY services/customer-service ./services/customer-service

WORKDIR /src/services/customer-service
RUN --mount=type=cache,target=/root/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 go build -o /out/customer-service ./cmd/server

FROM alpine:3.20
RUN adduser -D -u 10001 finverse
# Same reasoning as auth-service's Dockerfile: pre-own /data/documents before
# it's mounted as a named volume, so Docker's volume-seeding copies the right
# ownership in and internal/repository/localstorage can actually write to it
# as the non-root finverse user.
RUN mkdir -p /data/documents && chown finverse:finverse /data/documents
COPY --from=build /out/customer-service /usr/local/bin/customer-service
USER finverse
EXPOSE 8082
ENTRYPOINT ["/usr/local/bin/customer-service"]
