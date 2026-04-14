# Task Spec: task-2026-04-13-start-new-task

## Metadata
- Task ID: task-2026-04-13-start-new-task
- Created: 2026-04-13T11:44:51+00:00
- Spec frozen: 2026-04-13T12:00:00+00:00
- Repo root: /Users/orhanmamedov/Repos/personal/watgbridge
- Working directory at init: /Users/orhanmamedov/Repos/personal/watgbridge

## Guidance sources
- AGENTS.md
- features-plan.md
- telegram/handlers.go
- whatsapp/client.go
- whatsapp/wa_link_notify.go
- whatsapp/handlers.go
- mainbot/mainbot.go
- database/types.go
- database/bridges.go

## Original task statement
Ok, bot added to target group is not working:
- doesnt respond when I enter phone number to check if it exist
- doesnt forward incoming messages

On WA I don't see a session there, so we need new bridge-wide commands:
/linkinfo — shows the meta information on current WA session
/linkundo — closes current WA session
/link — restarts QR link flow
/linkhistory — shows information on previously active session

There could be only one active session per bridge

New (active) sessions should be seen at `List my bridges` in main bot

Follow-up:
- Bot appears non-responsive in group usage scenarios.
- Runtime logs contain: `failed to persist active whatsapp session state` with `no such column: wa_session_jid`.
- `List my bridges` output must expose explicit columns:
  `Forum Group ID | Bridge Bot ID | Bridge Bot Nickname | WA Session Name | WA Session Status`.

## Acceptance criteria
- AC1: Telegram bridge bot registers and handles new owner-only commands `/linkinfo`, `/linkundo`, `/link`, and `/linkhistory`.
- AC2: `/linkinfo` returns current session metadata in Telegram (linked/not linked status and available session identifiers for the currently active WA session).
- AC3: `/linkundo` cleanly logs out and disconnects the current WA session, updates persisted session state to non-active, and posts an actionable reconnect path.
- AC4: `/link` starts a fresh QR linking flow for the bridge and handles "already linked" and "reconnect in progress" conditions with clear responses.
- AC5: Session history metadata is persisted per bridge and exposed via `/linkhistory`, including previous linked session details after relink/logout events.
- AC6: Main bot `List my bridges` output includes active WA session visibility for each bridge, sourced from shared registry-visible bridge state.
- AC7: Existing behavior (forum provisioning, standard forwarding pipeline, existing commands) is not regressed by the new session features.
- AC8: Bridge child startup auto-migrates `bridge_provision_states` on the shared registry DB so session persistence does not fail with missing-column errors.
- AC9: Main bot bridge listing text follows the requested explicit column contract:
  `Forum Group ID | Bridge Bot ID | Bridge Bot Nickname | WA Session Name | WA Session Status`.

## Constraints
- Keep compatibility with single and multi mode; main-bot visibility must work in multi mode where bridge runtime is a child process.
- Use repository conventions and existing DB migration approach (`AutoMigrate` only; additive schema change only).
- Keep one active WA session per bridge runtime; do not introduce multi-session support.
- Preserve owner/sudo authorization model used by existing bridge commands.
- Do not remove CGO/SQLCipher assumptions.

## Non-goals
- No redesign of onboarding UX or broader bridge provisioning flow.
- No guarantee of automatic recovery for all WhatsApp-side outages beyond the new manual commands.
- No changes to unrelated command semantics beyond minimal wording updates if needed.
- No introduction of automated tests (repo currently has no test suite); verification is command/build based.

## Verification plan
- Build:
  - `go -C . build ./...`
- Unit tests:
  - None available in repo.
- Integration tests:
  - Not available; verify via code path inspection and command wiring checks.
- Lint:
  - `gofmt` on modified files.
- Manual checks:
  - Confirm `/linkinfo`, `/linkundo`, `/link`, `/linkhistory` are registered in bridge bot command list.
  - Confirm session metadata writes on link success and logout paths.
  - Confirm main bot `/bridge_list` text includes session status/details.
