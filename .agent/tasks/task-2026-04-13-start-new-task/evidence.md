# Evidence Bundle: task-2026-04-13-start-new-task

## Summary
- Overall status: UNKNOWN
- Last updated: 2026-04-13T12:00:00Z

## Acceptance criteria evidence

### AC1
- Status: PASS
- Proof:
  - Registered commands `/linkinfo`, `/linkundo`, `/link`, `/linkhistory` in `telegram.AddTelegramHandlers`.
  - File: `telegram/handlers.go`
- Gaps:
  - None in code wiring.

### AC2
- Status: PASS
- Proof:
  - Implemented `LinkInfoCommandHandler` with runtime + persisted session metadata output.
  - Implemented shared session state loader `whatsapp.LoadProvisionSessionState`.
  - Files: `telegram/handlers.go`, `whatsapp/session_state.go`
- Gaps:
  - Live runtime output not manually exercised in this run.

### AC3
- Status: PASS
- Proof:
  - Implemented `LinkUndoCommandHandler` using `waClient.Logout` and persisted inactive session snapshot.
  - Added inactive-session persistence helper `database.BridgeProvisionMarkSessionInactive`.
  - Files: `telegram/handlers.go`, `database/bridges.go`
- Gaps:
  - Live WA logout behavior not manually exercised in this run.

### AC4
- Status: PASS
- Proof:
  - Implemented `LinkCommandHandler` to reset active session and start new async QR reconnect flow.
  - Added reusable async launcher `whatsapp.StartWhatsAppQRReconnectAsync`.
  - Files: `telegram/handlers.go`, `whatsapp/wa_link_notify.go`
- Gaps:
  - Full manual QR cycle not exercised in this run.

### AC5
- Status: PASS
- Proof:
  - Added bridge session persistence fields for current + previous sessions in `BridgeProvisionState`.
  - Added `/linkhistory` command rendering previous session identity/timestamps/reason.
  - Files: `database/types.go`, `database/bridges.go`, `telegram/handlers.go`
- Gaps:
  - No manual relink/logout cycle performed to populate history.

### AC6
- Status: PASS
- Proof:
  - Updated main bot bridge listing to include `wa linked` / `wa not linked` state from bridge provision rows.
  - File: `mainbot/mainbot.go`
- Gaps:
  - Manual check in real main bot chat not executed in this run.

### AC7
- Status: UNKNOWN
- Proof:
  - `go build ./...` succeeds (see `raw/build.txt`).
  - Formatting pass completed (see `raw/lint.txt`).
- Gaps:
  - No live end-to-end manual validation with real WhatsApp + Telegram session to confirm forwarding behavior.

## Commands run
- `gofmt -w database/bridges.go database/types.go whatsapp/session_state.go whatsapp/client.go whatsapp/wa_link_notify.go whatsapp/handlers.go telegram/handlers.go mainbot/mainbot.go`
- `go build ./...`

## Raw artifacts
- `.agent/tasks/task-2026-04-13-start-new-task/raw/build.txt`
- `.agent/tasks/task-2026-04-13-start-new-task/raw/test-unit.txt`
- `.agent/tasks/task-2026-04-13-start-new-task/raw/test-integration.txt`
- `.agent/tasks/task-2026-04-13-start-new-task/raw/lint.txt`
- `.agent/tasks/task-2026-04-13-start-new-task/raw/screenshot-1.png`

## Known gaps
- Real-session manual verification is still required for forwarding and phone-check behavior.
