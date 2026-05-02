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
  pongs, and read deadlines wired. The default upgrader enforces a
  same-origin `CheckOrigin` (mitigates cross-site WebSocket hijacking) —
  use `sugaar.AllowOrigins(...)` or a custom `websocket.Upgrader` to opt
  into cross-origin callers.
- **SSE.** `app.SSETopic` does the same over `text/event-stream` for
  `EventSource` and `curl -N`. `app.SSETopicWith(fn, sugaar.SSEOptions{...})`
  adds heartbeats, a client `retry:` hint, write deadlines, and
  `Last-Event-ID` resume against the Hub replay buffer. See
  `examples/sse-resilient`.
- **Static files.** `app.Static("/assets", "./public")` or `StaticFS` over
  `embed.FS`.
- **Bind helpers.** `BindJSON`, `BindQuery`, `BindForm` with struct tags.
- **HTTPS.** `Options.AutoCertDomains` enables Let's Encrypt; `CertFile`/
  `KeyFile` for static certs; HTTP redirects to HTTPS automatically. TLS
  defaults: TLS 1.2+, ALPN `h2`/`http/1.1`/`acme-tls/1`, key cache mode 0700.
- **pprof.** Mounted at `/debug/pprof` by default, but **gated to loopback
  callers**. Override with `Options.PprofAuth` (any `Authenticator`) to
  expose it behind authentication, or set `Options.DisablePprof`.
- **Health.** `/healthz` returns `200 ok` for load balancers and Docker
  HEALTHCHECK. Disable with `Options.DisableHealth`.
- **Hardened HTTP server.** Production timeouts (`ReadHeaderTimeout`,
  `ReadTimeout`, `IdleTimeout`, `MaxHeaderBytes`) and a 1 MiB request-body
  cap (`Options.MaxBodyBytes`, applied by `BindJSON` / `BindForm` →
  413 Payload Too Large on overflow). Streaming endpoints (SSE/WS) run
  without a server `WriteTimeout`; per-write deadlines are managed inside
  the handlers via `http.ResponseController`.
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

## Authentication & Authorization

sugaar ships with a pluggable auth layer: implement `Authenticator` (prove identity)
or `Authorizer` (check permissions) and wrap routes with `sugaar.Auth(...)`.

```go
// API-key auth
api := app.Group("/api")
api.Use(sugaar.Auth(sugaar.APIKeyAuth("X-API-Key", "api_key", keyStore.Verify)))

// Bearer token with a custom verifier
app.Use(sugaar.Auth(sugaar.BearerAuthAuthenticator(tokenStore.Verify)))

// JWT (sub-package; adds github.com/golang-jwt/jwt/v5)
import "github.com/eneko/sugaar/jwtauth"
jwt := jwtauth.New([]byte(os.Getenv("JWT_SECRET")),
    jwtauth.WithClaimsMap(func(m map[string]any) *sugaar.Identity {
        return &sugaar.Identity{Subject: m["sub"].(string), Roles: m["roles"].([]string)}
    }),
)
app.Use(sugaar.Auth(jwt))

// Role-based access control
admin := app.Group("/admin", sugaar.RequireRoles("admin"))
admin.POST("/restart", restartHandler)

// Composite auth — API key *or* Bearer
app.Use(sugaar.Auth(sugaar.AnyOf(
    sugaar.APIKeyAuth("X-API-Key", "", apiKeyStore.Verify),
    sugaar.BearerAuthAuthenticator(bearerStore.Verify),
)))

// Handler reads the authenticated identity
app.GET("/me", func(c *sugaar.Context) error {
    id, _ := c.Identity()
    return c.JSON(200, map[string]string{"user": id.Subject})
})
```

The old `BasicAuth(user, pass)` and `BearerAuth(tokens...)` middleware still work
unchanged; they are now thin wrappers over the new built-in authenticators.

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

`pprof` is on by default but **rejects non-loopback callers** with 403, so
publicly-bound servers don't leak heap dumps. Profile from the box itself:

```
ssh vps -- go tool pprof -http=:0 http://127.0.0.1:8080/debug/pprof/profile?seconds=30
```

Or expose it behind authentication for remote access:

```go
app := sugaar.New(sugaar.Options{
    PprofAuth: sugaar.BasicAuthAuthenticator(func(u, p string) (*sugaar.Identity, error) {
        if u == os.Getenv("PPROF_USER") && p == os.Getenv("PPROF_PASS") {
            return &sugaar.Identity{Subject: u}, nil
        }
        return nil, errors.New("denied")
    }),
})
```

## Production checklist

- [ ] Set `Options.AutoCertDomains` (or `CertFile`/`KeyFile`) so HTTPS is on.
- [ ] Run as non-root; bind 80/443 via `AmbientCapabilities=CAP_NET_BIND_SERVICE`.
- [ ] Decide `Options.MaxBodyBytes` for your largest legitimate payload.
- [ ] Decide `Options.PprofAuth` (or keep loopback-only).
- [ ] If exposing WebSocket to browsers on a different origin, set
      `AllowOrigins(...)` in a custom upgrader passed to `StreamTopicWith`.
- [ ] Add a CDN/reverse proxy in front for static assets and rate limits.
- [ ] Wire `/healthz` to your orchestrator's liveness/readiness probe.
```
