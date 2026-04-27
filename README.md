# sugaar

A minimal Go web framework for streaming agentic events. Built on stdlib
`net/http` (Go 1.22+ pattern router); zero third-party router or context
deps. Inspired by gin's pooled-context style, but smaller and tailored to
WebSocket/SSE workloads on a VPS.

## Why

You're running an agent on a VPS and want to fan its events out to many
clients in real time. You want one binary, HTTPS without ops, pprof you can
hit live, and tests that diff against committed truth so reviewers see
exactly what changed.

## Quick start

```go
package main

import (
    "context"
    "github.com/eneko/sugaar"
)

func main() {
    app := sugaar.New(sugaar.Options{
        AutoCertDomains: []string{"agents.example.com"}, // optional
    })

    app.GET("/healthz", func(c *sugaar.Context) error {
        return c.String(200, "ok")
    })

    // Stream Hub events for a topic, both ways.
    topic := func(c *sugaar.Context) string { return "agents." + c.Param("id") }
    app.GET("/ws/agents/{id}",  app.StreamTopic(topic))
    app.GET("/sse/agents/{id}", app.SSETopic(topic))

    // Publish from anywhere — the agent goroutine, an HTTP POST, etc.
    go app.Hub.Publish(sugaar.Event{Topic: "agents.42", Type: "thought", Data: "hi"})

    app.Run(context.Background())
}
```

## Features

- **Routing.** `app.GET("/users/{id}", ...)` via stdlib `ServeMux` (Go 1.22).
  No router dependency.
- **Route groups.** `app.Group("/api/v1", BearerAuth(...))` with nested
  groups and group-scoped middleware.
- **Pooled `Context`.** Reused across requests via `sync.Pool`. Handlers
  return `error`, so the error path is one explicit place.
- **Typed errors.** `sugaar.NotFound`, `BadRequest`, `Unauthorized`, etc.
  Return them and the client gets the right status with a JSON body.
- **Middleware out of the box.** `RequestID`, `CORS`, `Timeout`, `BasicAuth`,
  `BearerAuth`, `GZip`. App-level middleware wraps the entire mux; group
  middleware wraps only matched routes.
- **Event Hub.** Topic-based fan-out with per-subscriber buffering and drop
  counts when subscribers are slow. Optional ring-buffer **replay** so a
  reconnecting client picks up where it left off.
- **WebSocket.** `app.StreamTopic` upgrades and forwards Hub events; pings,
  pongs, and read deadlines wired.
- **SSE.** `app.SSETopic` does the same over `text/event-stream` for
  `EventSource` and `curl -N`.
- **Static files.** `app.Static("/assets", "./public")` or `StaticFS` over
  `embed.FS`.
- **Bind helpers.** `BindJSON`, `BindQuery`, `BindForm` with struct tags.
- **HTTPS.** `Options.AutoCertDomains` enables Let's Encrypt; `CertFile`/
  `KeyFile` for static certs; HTTP redirects to HTTPS automatically.
- **pprof.** Mounted at `/debug/pprof` by default. `make profile` grabs 30s.
- **Graceful shutdown.** `Run` honors context and SIGINT/SIGTERM.
- **Test client.** `sugaartest.New(app).GET("/u/42")` returns
  `*http.Response`, ready for golden assertions.
- **gRPC.** `app.EnableGRPC(":9090")` returns a `*grpc.Server` you register
  services on. Reflection on; lifecycle bound to `app.Run`.
- **Wire codecs.** `sugaar/codec` ships Protobuf and Avro helpers
  (`Proto`, `BindProto`, `Avro`, `BindAvro`) plus `Render` for content
  negotiation across JSON / proto / Avro from a single handler.

## gRPC + content negotiation

```go
app := sugaar.New(sugaar.Options{Addr: ":8080"})
g   := app.EnableGRPC(":9090")
pb.RegisterAgentsServer(g.Server, &agentsImpl{Hub: app.Hub})

// Same handler, three wire formats — chosen by Accept.
app.GET("/agents/{id}", func(c *sugaar.Context) error {
    ev := loadEvent(c.Param("id"))
    return codec.Render(c, 200, ev, codec.AvroSchema(agentEventSchema))
})

app.Run(ctx)
```

## Make-driven DX

`make help` lists everything. Highlights:

| target          | what                                                   |
| --------------- | ------------------------------------------------------ |
| `make check`    | fmt + vet + race tests — pre-push gate                 |
| `make dev`      | live-reload (via `air`)                                |
| `make profile`  | grab a 30s CPU profile and open it in pprof's web UI   |
| `make trace`    | grab a 5s execution trace                              |
| `make proto`    | regenerate Go from `proto/*.proto`                     |
| `make avro`     | regenerate Go from `avro/*.avsc`                       |
| `make tls-dev`  | mint a self-signed cert for local HTTPS                |
| `make bin-linux`| static linux binary for VPS                            |
| `make docker`   | scratch image                                          |
| `make deploy`   | scp the binary + restart systemd on `$VPS_HOST`        |
| `make systemd-unit` | print a starter unit file                          |

## Tests as approved truth

`sugaar/golden` writes plain-text "transcripts" of HTTP responses or event
streams and diffs them against committed files. Reviewers see truth as text
in PRs, not opaque assertions.

```go
rec := httptest.NewRecorder()
app.ServeHTTP(rec, httptest.NewRequest("GET", "/hello/eneko", nil))
golden.Assert(t, "hello", golden.Response(rec.Result()))
```

`testdata/hello.golden.txt`:

```
< 200 OK
< Content-Type: application/json; charset=utf-8
---
{
  "hello": "eneko"
}
```

When intent changes, run `make update-golden` and review the diff in git.

## Deployment

It's one binary. Drop it on a VPS, point a DNS A record at the box, set
`AutoCertDomains`, run with systemd. Example unit:

```ini
[Unit]
Description=agent-stream
After=network.target

[Service]
ExecStart=/usr/local/bin/agent-stream
Restart=on-failure
AmbientCapabilities=CAP_NET_BIND_SERVICE
User=agents

[Install]
WantedBy=multi-user.target
```

## Profiling a live VPS

`pprof` is on by default:

```
go tool pprof -http=:0 https://your-host/debug/pprof/profile?seconds=30
```

Restrict the route in production with a middleware or a reverse-proxy ACL.
```
