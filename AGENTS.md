# Agent notes

All planned work, backlog items, and what shipped is **tracked in [`features-plan.md`](features-plan.md)**. Read that file before large changes so scope stays aligned.

---

## Project overview

**WaTgBridge** is a Go application that forwards WhatsApp messages to Telegram and allows replies from Telegram back to WhatsApp. It is not a true two-way bridge — direction is WhatsApp → Telegram with reply support.

Two operational modes:
- **Single mode** (legacy): one bridge instance per deployment, configured via a single `config.yaml`.
- **Multi mode**: a central "main bot" manages multiple user bridge instances as child processes; each child gets its own config slice via `bridge_provision_states` in the DB.

---

## Build

Requires CGO (for SQLCipher):

```bash
CGO_ENABLED=1 go build -o watgbridge
```

External tools needed at runtime: `ffmpeg`, `imagemagick` (optional, for media conversion).

Cross-compile for arm64:
```bash
CC=aarch64-linux-gnu-gcc CXX=aarch64-linux-gnu-g++ \
  CGO_ENABLED=1 GOOS=linux GOARCH=arm64 go build
```

Docker (recommended):
```bash
./run.sh --telegram-bot-token 'TOKEN' --telegram-owner-id 'ID' --telegram-target-chat-id '-100...'
```

No automated test suite — testing is manual via a real WhatsApp account and Telegram bot.

---

## Repository layout

```
main.go                  # Entry point: flags → config → DB → Telegram client → WhatsApp client → idle
state/                   # Global state and config (Viper cascade: file → env → CLI flags)
  config.go              # Config struct, YAML load/save
  state.go               # Global State struct and initialization
  viper.go               # Viper integration
database/                # GORM persistence layer
  connect.go             # Multi-DB support: SQLite (default, optionally SQLCipher), Postgres, MySQL
  types.go               # GORM models: chats, messages, contacts
  helpers.go             # Query helpers (message ID pairs, thread lookups, contacts)
  bridges.go             # Bridge state for multi mode
telegram/                # Telegram bot client and command handlers
  client.go              # Bot init, polling
  handlers.go            # All /commands
whatsapp/                # WhatsApp client and event handling
  client.go              # whatsmeow setup, QR auth
  handlers.go            # Event dispatcher (messages, reactions, presence, group info, …)
utils/                   # Shared helpers
  telegram.go            # TG utilities: TgSendToWhatsApp, TgGetOrMakeThreadFromWa, TgReplyTextByContext
  whatsapp.go            # WA utilities: WaParseJID, WaFuzzyFindContacts, WaSendText, WaTagAll
  stickers.go            # TGS↔WebP, WebM↔WebP, animated GIF conversion
bridge/                  # Multi mode: child bridge process management
mainbot/                 # Multi mode: main/control bot handlers
crypto/sqlitekey/        # SQLCipher HKDF key derivation + PRAGMA helpers
internal/gormsqlcipher/  # Custom GORM dialector for go-sqlcipher
docker/                  # Dockerfile entrypoint + config template
sample_config.yaml       # Fully commented config reference
features-plan.md         # Backlog and shipped features (source of truth)
```

---

## Key conventions

- **Message forwarding flow**: WhatsApp event → `whatsapp/handlers.go` → `utils/whatsapp.go` / `utils/telegram.go` → `TgGetOrMakeThreadFromWa` (creates a forum topic per WA chat if needed) → sends to Telegram.
- **Reply flow**: Telegram reply → `telegram/handlers.go` → `TgSendToWhatsApp` → WA client.
- **Thread mapping**: each WhatsApp chat JID maps to a Telegram forum topic thread ID stored in the `chats` table. Helper `TgGetOrMakeThreadFromWa` looks up or creates this mapping.
- **Message ID pairs**: every bridged message stores a `(wa_message_id, tg_message_id)` pair in the DB so edits and revokes can find the counterpart.
- **Config loading**: Viper cascade — config file → environment variables → CLI flags. The `state` package owns this; avoid duplicating config reads elsewhere.
- **Logging**: `go.uber.org/zap` structured logger accessed via `state.State.Logger`.
- **Multi mode child config**: child bridge configs are built from `bridge_provision_states` rows and written as temporary YAML before spawning the child process.

---

## Behaviour decisions (shipped — see features-plan.md for detail)

- Group message headers: `👤 name (+phone)`
- Forwarded messages: append `⏩: Forwarded N times` at the bottom
- Edits: reply to original TG message with bold `Edited`, then send updated content separately
- Revokes: reply to original TG message with bold `Deleted`
- Status (`status@broadcast`): dedicated **Status** forum topic; thread ID stored in config as `status_thread_id`
- Multi mode launch: send `Launched • version: …` to every registered user on start

---

## Important notes for agents

- **CGO is required** — do not remove or conditionally disable CGO; the SQLCipher dependency will fail to compile without it.
- **No test suite** — there are no `_test.go` files to run. Validate changes by reading the code paths carefully; do not claim tests pass.
- **whatsmeow version** — the WA client library (`go.mau.fi/whatsmeow`) is pinned to a specific Feb 2026 commit. Avoid bumping it without understanding API surface changes.
- **Database migrations** — GORM `AutoMigrate` is used; adding fields to models in `database/types.go` is safe but removing or renaming columns requires explicit migration logic.
- **Single vs multi mode** — many code paths branch on `state.State.Config.Telegram.Mode`. When adding features, consider whether they apply to one or both modes.
- **Docker data** — runtime data (DB, WA session, config) lives in `./data/` bind-mounted into the container. Do not hardcode paths inside the container that differ from this.

<!-- repo-task-proof-loop:start -->
## Repo task proof loop

For substantial features, refactors, and bug fixes, use the repo-task-proof-loop workflow.

Required artifact path:
- Keep all task artifacts in `.agent/tasks/<TASK_ID>/` inside this repository.

Required sequence:
1. Freeze `.agent/tasks/<TASK_ID>/spec.md` before implementation.
2. Implement against explicit acceptance criteria (`AC1`, `AC2`, ...).
3. Create `evidence.md`, `evidence.json`, and raw artifacts.
4. Run a fresh verification pass against the current codebase and rerun checks.
5. If verification is not `PASS`, write `problems.md`, apply the smallest safe fix, and reverify.

Hard rules:
- Do not claim completion unless every acceptance criterion is `PASS`.
- Verifiers judge current code and current command results, not prior chat claims.
- Fixers should make the smallest defensible diff.

Installed workflow agents:
- `.codex/agents/task-spec-freezer.toml`
- `.codex/agents/task-builder.toml`
- `.codex/agents/task-verifier.toml`
- `.codex/agents/task-fixer.toml`
<!-- repo-task-proof-loop:end -->
