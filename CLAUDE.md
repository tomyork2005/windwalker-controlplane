# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Commands

Run / build / check — from the repo root:

```bash
# Run the service (CONFIG_PATH is REQUIRED — MustLoadConfig hard-fails without it)
CONFIG_PATH=config/config.yaml go run ./cmd

go build ./...
go vet ./...
go test ./...                           # run all tests
go test ./internal/service/... -run TestX  # single package / single test
```

Migrations use **goose** format (`-- +goose Up` / `-- +goose Down` markers in `migrations/*.sql`) — apply with:

```bash
goose -dir migrations postgres "postgres://postgres:postgres@127.0.0.1:5433/control_plane?sslmode=disable" up
```

Proto changes: `api/control/control.proto` is the source; `control.pb.go` and `control_grpc.pb.go` are generated (regenerate via `protoc --go_out --go-grpc_out` with go_package `control/api/control;controlpb`).

## Architecture

This is a VPN **control plane**: a Telegram shop + payment processor that provisions VPN credentials by dispatching commands to remote **agents** over a bidirectional gRPC stream. A single `cmd/main.go` binary wires up four concurrent servers via `errgroup`: HTTP (chi), gRPC, Telegram webhook, and an outbox worker.

### End-to-end flow

1. **Telegram bot** (`internal/transport/telegram-bot`) drives the purchase flow: user picks region → protocol → duration → payment method. `ShopService.CreateInvoice` calls `Payments.CreatePaymentOrder` (routed by `methodID` to a `payment.Provider` impl, e.g. `cryptocloud`), persists an `Invoice` with status `created`, and returns a checkout URL.
2. **Provider webhook** (HTTP, `internal/transport/handler`) receives payment callback → `ProcessService.ProcessPaymentCallback`:
   - `VerifyCallback` on the `Payments` facade
   - Inside `WithTx`: `SELECT … FOR UPDATE` the invoice, mark `success`, create `Subscription`, write a `SubscriptionActivatedEvent` to `outbox_events` (**transactional outbox**).
3. **Outbox worker** (`internal/workers`) polls `outbox_events` every 3s in batches of 5 (max 10 attempts). For `subscription_activated` it invokes `AgentSender.StartUserSubscribe`, which — inside a tx — calls `ChooseBestAgent` (region + driver_type, least-loaded), `BindSubscriptionToAgent`, then `Dispatcher.DispatchUpsert`. After commit it calls `TryDispatchPrepared` to push immediately.
4. **Dispatcher** (`internal/service/dispatcher.go`) is the durable command pipe. `EnqueueTask` writes to `agent_tasks` with a monotonic `seq` from `NextSeq` (backed by `agent_seq` table — note this table is **not in migrations/**, it must exist out-of-band). `TrySend` is non-blocking over the agent's in-memory `sendCh`; failure marks retries but the row stays pending.
5. **Agent gRPC Workstream** (`internal/transport/agent/server.go`):
   - Agent opens bidi stream, first frame **must** be `AgentHello` → `RegisterAgent` → creates a `session` in `Hub` (swapping out any stale session for the same `agent_id`).
   - Server replies `Welcome`, then `flushPending` kicks off: `RecoverPending` fetches unacked `agent_tasks` from `lastSeq`, sends them in order, marks `sent_at`.
   - Client responses update `agent_tasks` via `HandleAck` / `HandleNack`. Heartbeats reset a per-stream `hbTimer`; missing heartbeat → stream closes with `DeadlineExceeded`.
   - On `HandleStartUserSubscribeResponse` (upsert reply with creds), credentials are sent to the user via `TelegramSender` using `ResolveChatIDBySubscribeID`.

### Idempotency & ordering contract

- `request_id` (= `subscription_id` for subscribe flows) = **business** idempotency key.
- `(agent_id, seq)` uniquely identifies a transport operation; the agent is expected to process strictly in-order and ack by `seq`.
- Same `(agent_id, seq)` may be re-sent after agent reconnect — agent must be idempotent by `(request_id, seq)`.
- Invoice webhook is idempotent via `status == SuccessInvoiceStatus` short-circuit.

### Transaction propagation

`pgx.TxManager.WithTx` stashes the `pgx.Tx` in `context.Context`; every storage method calls `s.getExecutor(ctx)` which returns the tx if present, else the pool. **Always call storage methods with the tx-bearing ctx** inside `WithTx` callbacks — never the outer ctx, or you'll silently split a logical transaction. Nested `WithTx` reuses the existing tx (no savepoints).

### Layering

- `cmd/main.go` — composition root; all wiring lives here, not in package inits.
- `internal/model` — pure domain types + JSON payloads for outbox / task rows. No I/O, no framework deps.
- `internal/service` — use-cases. Each service defines **its own narrow storage / collaborator interfaces** right above the struct — the concrete `pgx.Storage` happens to satisfy all of them. When adding a service method, extend the matching interface in the consumer package, not a central one.
- `internal/storage/pgx` — pgx implementations. Uses `scany` for row scanning. `db:` struct tags on model types drive scanning.
- `internal/transport/{agent,handler,telegram-bot}` — gRPC, HTTP, Telegram adapters. Converters (proto ↔ model) live next to the transport, not in `model`.
- `internal/payment` — provider registry keyed by both provider name (for webhook routing) and method id (for order creation). Add a provider by implementing `payment.Provider` and passing it to `NewPayments(...)` in `main.go`.
- `internal/workers` — outbox dispatcher loop. New event types: add a const in `model/outbox.go`, a payload type, a case in `Worker.resolveEvent`.
- `api/control` — protobuf contract with agents. `go_package` is `control/api/control;controlpb`.

### Config

`internal/config` uses `cleanenv` with YAML + env overrides. `CONFIG_PATH` env var is **required** — absent/missing file = `log.Fatal`. The repo's `config/config.yaml` is gitignored in spirit (note `.gitignore` has `./config/config.yaml` but the file is currently committed with real-looking secrets — treat it as a local dev template; don't commit real credentials).

## Known rough edges (don't "fix" unless asked)

- `agent_seq` table is referenced by `NextSeq` but has no migration file.
- `Agent.Validate()` is a no-op stub.
- Several `HandleRemoveCallback` / `HandleStatsAll` / `HandleError` methods return `"not implemented yet"`.
- `freekassa` payment provider exists but is not wired in `main.go`.
- HTTP `http.port` and telegram `telegram.port` both default to `:8081` in the sample config — telegram's `port` field is unused at runtime (webhook is mounted on the chi router at `/tg/webhook`).