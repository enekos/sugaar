# Two-stage scratch image for the agent-stream example.
# Override EXAMPLE with --build-arg to package a different cmd.
FROM golang:1.22-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
ARG EXAMPLE=./examples/agent-stream
RUN CGO_ENABLED=0 go build -ldflags="-s -w" -o /out/app ${EXAMPLE}

FROM scratch
COPY --from=build /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --from=build /out/app /app
EXPOSE 8080 8443 9090
ENTRYPOINT ["/app"]
