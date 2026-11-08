# Long-running Research Agent streams — issue #397

## Why the old behavior broke

The old `POST /api/ai/chat` ran `ai.Run` directly on `r.Context()`. A mobile-network dropout, reverse-proxy stream limit, browser tab closure or client-side `ReadableStream` error could cancel the context and kill an otherwise healthy model run. The 10-second SSE heartbeat from #387 helps with *idle connections* but cannot make a cancelled request resume.

## Run lifecycle

1. Resolve the authenticated user, model/profile, evidence policy and budgets **before** starting work. Generate a cryptographically random opaque run id (192 bits), enforce up to 2 simultaneous runs per user and 80 total, and detach the model context from the HTTP request.
2. Start the run under its own 90-minute deadline. Give long reasoning model calls an explicit 15-minute idle budget and a 45-minute per-call maximum. These apply only to the Research Agent chat endpoint.
3. Append model, thinking, tool and provenance events to a mutex-protected server-side journal, with monotonically increasing SSE `id` and a final `stream_end`. No browser write is part of the model execution.
4. The initial POST streams journal entries and includes `X-Agent-Run-ID`. If the connection breaks, the browser retries a **GET** to `/api/ai/runs/{id}/events?after={lastId}`, not another POST. Older frames cannot be duplicated and model/tools remain single-execution.
5. Explicit Stop calls `DELETE /api/ai/runs/{id}` to cancel the server run. A disconnected browser only closes its own subscriber. The GET and DELETE routes both verify the authenticated owner's ID.
6. The journal is bounded to 16,384 events, with 1-hour retention after completion (or less under global memory pressure). A stale cursor receives HTTP 410 rather than a silently truncated answer; an unknown/unauthorized run receives 404. A browser makes up to 450 bounded-backoff reconnect attempts (roughly a 90-minute outage window) and shows a clear reconnection status; explicit Stop interrupts the backoff immediately. Transient 429/502/503/504 on idempotent reconnect GETs can be retried, but the initial POST is never resent automatically.

## Tradeoffs and operational limitations

This is **in-memory resilience for transient disconnects**, not distributed/durable asynchronous execution. A process restart, pod replacement or a load balancer moving a reconnect to another instance loses the journal. The server should use sticky routing or a shared durable stream store to span replicas; persisted deliverable tasks retain their existing independent checkpoints. This does not claim crash-proof recovery.

Large responses have bounded replay memory and run lifetime. Transient network disconnections do not duplicate market/provider calls or tool side effects. User-initiated Stop cancels server resources, whereas a closed TCP connection does not. Resource ceilings are deliberate; a slow model can still fail when its configured timeout or the run budget is exhausted.

## Verification

- `go test ./internal/server -run 'TestDetachedChat|TestChatRun' -count=1`
- `node --test frontend/src/app/core/agent-stream.spec.ts`
- Full GitHub Actions frontend, Go race, policy and schema jobs.
- Manual staging validation: allow a local reasoning model to pause for several minutes, sever the browser network during streaming, restore the network and confirm a single response and tool ledger without a second model request. Separately verify explicit Stop and a timed-out run.

**Status:** Implementation and deterministic tests on PR #396. No live proxy/device test or production deployment is claimed until independently performed.
