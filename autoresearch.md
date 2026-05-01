# Autoresearch: HTTP and Network Stack Optimizations

## Objective
Find and implement performance improvements in the sugaar Go web framework's HTTP handling, routing, middleware, SSE, WebSocket, and Hub pub/sub stack. Focus on reducing latency, allocations, and lock contention.

## Metrics
- **Primary**: `route_ns` (ns/op, lower is better) — weighted composite of route benchmarks
- **Secondary**:
  - `hub_ns` (ns/op) — Hub fanout performance
  - `route_allocs` (allocs/op) — allocation count for routing
  - `hub_allocs` (allocs/op) — allocation count for hub operations

## How to Run
`./autoresearch.sh` — outputs `METRIC name=number` lines.

## Files in Scope
- `sugaar.go` — App, ServeHTTP, routing, server setup, statusWriter
- `context.go` — Context, HandlerFunc, Middleware, JSON/String/Status helpers, contextPool
- `hub.go` — Hub, Subscription, Publish, Subscribe fanout
- `hub_replay.go` — replayBuffer for event replay
- `sse.go` — SSE streaming, writeSSE, SSETopic handler
- `websocket.go` — WSConn, StreamTopic, WebSocket upgrade
- `middleware.go` — GZip, CORS, Timeout, RequestID, BasicAuth, BearerAuth
- `event.go` — Event struct and MarshalJSON
- `group.go` — Group routing
- `autoresearch_bench_test.go` — benchmark harness

## Off Limits
- `codec/`, `avro/`, `proto/`, `jwtauth/`, `internal/pb/` — not HTTP/network core
- `grpc.go` — gRPC server setup (not the hot path)
- Test files except `autoresearch_bench_test.go` (which we own)
- Public API signatures must remain compatible

## Constraints
- All existing tests must pass (`go test ./...`)
- No new external dependencies
- Go 1.24+ compatibility required
- Benchmarks must not cheat (e.g., no skipping actual work)

## What's Been Tried
*(Empty — session just started)*

## Ideas
- Hub.Publish allocates subs slice per call — could use sync.Pool
- statusWriter.Write does atomic-like int addition on every write
- requestLogMiddleware always runs even with discard logger — check if we can skip
- writeSSE uses fmt.Fprintf per field — could use byte buffer pooling
- Context.JSON creates json.NewEncoder each time — could pool or use json.Marshal
- Event.MarshalJSON calls time.Now().UTC() unconditionally
- Hub.Publish takes write lock briefly but then releases and iterates — contention pattern
