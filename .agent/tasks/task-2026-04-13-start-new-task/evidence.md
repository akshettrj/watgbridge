# Evidence Bundle: task-2026-04-13-start-new-task

## Summary
- Overall status: UNKNOWN
- Last updated: 2026-04-14T00:00:00Z

## Acceptance criteria evidence

### AC1
- Status: PASS
- Proof:
  - Registered commands `/linkinfo`, `/linkundo`, `/link`, `/linkhistory`.
  - File: `telegram/handlers.go`
- Gaps:
  - None in wiring.

### AC2
- Status: PASS
- Proof:
  - `LinkInfoCommandHandler` reports runtime and persisted WA session metadata.
  - Files: `telegram/handlers.go`, `whatsapp/session_state.go`
- Gaps:
  - Manual runtime invocation not captured here.

### AC3
- Status: PASS
- Proof:
  - `/linkundo` uses `waClient.Logout` + persisted inactive-session snapshot.
  - Files: `telegram/handlers.go`, `database/bridges.go`
- Gaps:
  - Manual logout flow not executed in this run.

### AC4
- Status: PASS
- Proof:
  - `/link` resets linked session (if active) and starts async QR flow (`StartWhatsAppQRReconnectAsync`).
  - Files: `telegram/handlers.go`, `whatsapp/wa_link_notify.go`
- Gaps:
  - Full QR relink cycle not exercised in this run.

### AC5
- Status: PASS
- Proof:
  - Session history fields persisted in `BridgeProvisionState` and shown via `/linkhistory`.
  - Files: `database/types.go`, `database/bridges.go`, `telegram/handlers.go`
- Gaps:
  - No real history row populated during this run.

### AC6
- Status: PASS
- Proof:
  - Main bot listing includes WA session status sourced from provision state.
  - File: `mainbot/mainbot.go`
- Gaps:
  - Manual main-bot execution not captured in this run.

### AC7
- Status: UNKNOWN
- Proof:
  - `go build ./...` passes.
- Gaps:
  - End-to-end live forwarding verification was not run here.

### AC8
- Status: PASS
- Proof:
  - Added `AutoMigrateProvisionState` and invoked it for child-process registry DB binding.
  - Files: `database/types.go`, `main.go`
- Gaps:
  - Requires container restart in deployed environment to apply migration there.

### AC9
- Status: PASS
- Proof:
  - `bridge_list` output now follows explicit columns:
    `Forum Group ID | Bridge Bot ID | Bridge Bot Nickname | WA Session Name | WA Session Status`.
  - Files: `mainbot/mainbot.go`, `database/types.go`, `database/bridges.go`, `mainbot/bridge_add_core.go`
- Gaps:
  - Existing DB rows may show `unknown` nickname until identity backfill succeeds.

## Commands run
- `gofmt -w main.go mainbot/mainbot.go mainbot/bridge_add_core.go database/types.go database/bridges.go`
- `go build ./...`

## Raw artifacts
- `.agent/tasks/task-2026-04-13-start-new-task/raw/build.txt`
- `.agent/tasks/task-2026-04-13-start-new-task/raw/test-unit.txt`
- `.agent/tasks/task-2026-04-13-start-new-task/raw/test-integration.txt`
- `.agent/tasks/task-2026-04-13-start-new-task/raw/lint.txt`
- `.agent/tasks/task-2026-04-13-start-new-task/raw/screenshot-1.png`

## Known gaps
- Live Telegram/WhatsApp manual validation remains required.
