# sse-resilient

Demonstrates sugaar's resilient SSE handler.

## What you get

- **Heartbeats** every 5s as `: ping` comments — keeps proxies open and
  surfaces dead peers via the configured write timeout.
- **`retry:` directive** so browsers reconnect on a known cadence.
- **Replay buffer** of the last 100 events per topic, so a client that
  reconnects with `Last-Event-ID: N` picks up at `N+1` instead of missing
  whatever happened during its absence.
- **Drop callback** that logs whenever the per-connection buffer is full
  (i.e. the client can't keep up).

## Run

```sh
go run ./examples/sse-resilient
```

In another shell, watch the stream:

```sh
curl -N http://localhost:8080/sse/agents/42
```

You'll see frames like:

```
retry: 2000
: open

id: 1
event: thought
data: {"id":"1","topic":"agents.42","type":"thought","time":"...","data":"step 1"}

: ping

id: 2
event: thought
data: ...
```

## Resume after a drop

Pick an `id:` from the output (say `5`), kill the curl, then reconnect:

```sh
curl -N -H 'Last-Event-ID: 5' http://localhost:8080/sse/agents/42
```

The server replays everything published after `id=5` from the ring buffer,
then resumes live delivery.

## Browser client

```js
const es = new EventSource('/sse/agents/42');
es.addEventListener('thought', (e) => {
  const ev = JSON.parse(e.data);
  console.log(ev.id, ev.data);
});
```

`EventSource` automatically sends `Last-Event-ID` on reconnect, so resume is
handled for you.
