# syntax=docker/dockerfile:1.6
# Two-stage scratch image for the agent-stream example.
# Override EXAMPLE with --build-arg to package a different cmd.
FROM golang:1.24-alpine AS build
RUN apk add --no-cache ca-certificates tzdata
WORKDIR /src
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/root/.cache/go-build \
    --mount=type=cache,target=/go/pkg/mod \
    go mod download
COPY . .
ARG EXAMPLE=./examples/agent-stream
ARG VERSION=dev
RUN --mount=type=cache,target=/root/.cache/go-build \
    --mount=type=cache,target=/go/pkg/mod \
    CGO_ENABLED=0 go build -trimpath \
      -ldflags="-s -w -X main.version=${VERSION}" \
      -o /out/app ${EXAMPLE}

# Run as a non-root, dynamically-allocated user inside the scratch image.
# /etc/passwd entry is the simplest portable way to give the binary a home.
FROM scratch
COPY --from=build /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --from=build /usr/share/zoneinfo /usr/share/zoneinfo
COPY --from=build /out/app /app

USER 65532:65532
EXPOSE 8080 8443 9090

# Scratch has no shell or curl, so HEALTHCHECK is left to the orchestrator.
# sugaar mounts /healthz on Addr by default — wire k8s liveness/readiness
# probes or `docker run --health-cmd` against it.
ENTRYPOINT ["/app"]
