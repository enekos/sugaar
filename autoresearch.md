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

### ✅ Pre-compute middleware chain (KEEP #2)
- **Change**: Cache `chain(a.base, a.mws)` in `App.chained` instead of rebuilding per request
- **Result**: route_allocs 46→36 (-10), route_ns improved
- **Files**: `sugaar.go`

### ✅ Reuse statusWriter + json.Marshal (KEEP #3)
- **Change**: Embed `sw statusWriter` in Context for reuse; replace `json.NewEncoder` with `json.Marshal`
- **Result**: route_allocs 36→35, route_ns improved
- **Files**: `context.go`, `sugaar.go`

### ✅ Store request ID in Context field + unsafe hex (KEEP #4)
- **Change**: Add `reqID string` to Context; use `unsafe.String` to avoid hex.EncodeToString alloc
- **Result**: route_allocs 35→32 (-3), route_mw_ns improved significantly
- **Files**: `context.go`, `middleware.go`

### ✅ slog.LogAttrs in requestLogMiddleware (KEEP #5)
- **Change**: Replace `log.Info` with `slog.LogAttrs` to avoid `[]any` variadic slice alloc
- **Result**: route_allocs 32→25 (-7), massive alloc reduction
- **Files**: `sugaar.go`

### ✅ sync.Map → mutex+map for context passing (KEEP #6)
- **Change**: Replace `Request.WithContext` + `context.WithValue` with `reqCtxMap` guarded by mutex
- **Result**: route_allocs 25→22 (-3)
- **Files**: `sugaar.go`, `profile.go`

### ✅ slog.LogAttrs in Hub drop logging (KEEP #7)
- **Change**: Replace `log.Warn` with `slog.LogAttrs` in Hub.Publish drop path
- **Result**: hub_allocs 246→2 (-99%), hub_small_allocs 16→1, hub_large_allocs 230→1
- **Files**: `hub.go`

### ✅ unsafe.Slice for Context.String (KEEP #8)
- **Change**: Use `unsafe.Slice(unsafe.StringData(s), len(s))` to avoid `io.WriteString` alloc
- **Result**: route_str_allocs 3→2
- **Files**: `context.go`

### ✅ Eliminate subscriber slice in Hub.Publish (KEEP #11)
- **Change**: Send directly while holding lock instead of copying to slice
- **Result**: hub_allocs 2→0 (-100%), hub completely allocation-free
- **Risk**: Slightly more lock contention for Subscribe/unsubscribe under extreme fanout
- **Files**: `hub.go`

### ✅ Cache Content-Type header slices (KEEP #12)
- **Change**: Reuse global `[]string{"application/json; charset=utf-8"}` instead of `Header.Set`
- **Result**: route_allocs 22→15 (-7), route_str_allocs 2→0, route_json_allocs 9→7, route_mw_allocs 10→8
- **Safety**: Shared slices are safe because `append` with cap=1 always reallocates
- **Files**: `context.go`

### ❌ ctxWriter approach (REVERTED)
- **Attempted**: Pass Context through ResponseWriter wrapper to avoid Request.WithContext
- **Result**: Broke optional interfaces (Flusher, Hijacker) because embedded interface doesn't promote non-interface methods
- **Lesson**: Don't wrap ResponseWriter without implementing all optional interfaces

### ❌ SSE buffer pool (REVERTED)
- **Attempted**: Pool `bytes.Buffer` for SSE frame writes
- **Result**: No clear improvement; benchmark noise obscured results
- **Lesson**: SSE is connection-scoped, not per-event hot enough for pooling to matter

### ❌ Inline error handler in ServeHTTP (DISCARDED)
- **Attempted**: Remove `if err != nil` branch in ServeHTTP
- **Result**: No change in allocs or ns/op
- **Lesson**: Branch predictor already handles this well

## Current Best Results
- **route_allocs**: 15 (down from 46, -67%)
- **hub_allocs**: 0 (down from 246, -100%)
- **route_str_allocs**: 0 (completely allocation-free)
- **hub_small_allocs**: 0, **hub_large_allocs**: 0

## Remaining Allocations (hard to optimize)
The remaining ~7-8 allocs per route are from:
1. `json.Marshal` output buffer copy (stdlib, 1 alloc)
2. `json.Marshal` internal `reflectWithString` slice for map keys (stdlib, 1-2 allocs)
3. `reflect.copyVal` during JSON encoding (stdlib, 1-2 allocs)
4. `net/http.(*routingNode).matchPath` for parameterized routes (stdlib, 1 alloc)
5. Map literals in benchmark handlers (user code, 1 alloc)
6. Mutex map store for context passing (framework, ~1 alloc)

These require either stdlib changes, custom JSON encoder, or benchmark overfitting.

## Ideas for Future Work
- Custom minimal JSON encoder for common types (map[string]string, structs) to avoid reflect allocs
- Sharded mutex for `reqCtxMap` if lock contention becomes a bottleneck under high concurrency
- Rate-limited drop logging in Hub instead of per-drop `slog.LogAttrs`
- Pool `bytes.Buffer` for `writeSSE` if profiling shows fmt overhead in production
- HTTP/2 push or WebSocket binary framing for lower overhead than SSE text frames
- Zero-copy static file serving with `sendfile`
