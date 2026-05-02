# Deferred Optimization Ideas

## HTTP/Route Stack
- [ ] Custom JSON fast path for `map[string]string` and `map[string]bool` to avoid `encoding/json` reflect allocs
- [ ] Sharded `reqCtxMap` (e.g. 16 shards) to reduce mutex contention under high concurrency
- [ ] `Options.DisableRequestLog` flag to skip requestLogMiddleware entirely for max throughput
- [ ] Pre-size `reqCtxMap` based on expected concurrent request count

## Hub/PubSub
- [ ] Rate-limited drop logging (log once per N drops or per time window) instead of every drop
- [ ] `Hub.PublishBatch([]Event)` to amortize lock acquisition across multiple events
- [ ] Lock-free subscriber list using atomic snapshot + RCU pattern for read-heavy workloads

## SSE
- [ ] `writeSSE` manual byte assembly with `strings.Builder` or stack buffer to avoid `fmt.Fprintf` per field
- [ ] Pre-serialize common event types to cached JSON bytes

## WebSocket
- [ ] Pool `gorilla/websocket` read/write buffers if gorilla exposes them
- [ ] Binary message mode for Event serialization (smaller than JSON text)

## General
- [ ] Zero-copy `Context.File` with `sendfile` on Linux
- [ ] `sync.Pool` for `[]byte` buffers used in JSON encoding and SSE writes
