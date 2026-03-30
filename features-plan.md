# Features plan

**Source of truth** for backlog and shipped work. For AI/automation context, see [`AGENTS.md`](AGENTS.md).

---

## Shipped (recent)

### Bridge → Telegram behavior

- **Status** (`status@broadcast`): forum topic **Status**; mapping seeded like other meta topics (`status_thread_id` in config; DB row `status@broadcast` → thread id). Multi-bridge: provision creates a **Status** topic and stores `status_thread_id` in `bridge_provision_states` (child YAML gets `telegram.status_thread_id` when set).
- **Headers**: groups use `👤 name (+phone)`; status lines include contact + phone; private chats rely on topic title (no redundant headers).
- **Forwards**: `⏩: Forwarded N times` at the **bottom** of the bridged body/caption.
- **Edits**: reply to the original Telegram message with **`Edited`** (bold), then send updated content separately (no inline “edited” prefix in the main body).
- **Revokes**: reply to the original Telegram message with **`Deleted`** (bold).

### Main bot (multi mode)

- **Launch line**: `Launched • version: …` sent to **every registry user** (union of `bridge_users`, bridge `owner_user_id`, plus config `owner_id` / `sudo_users_id`, deduped). **`/start`** ensures the user is in `bridge_users` for future launches.
- **Removed**: bridge bot DM **`Successfully started WaTgBridge`** to the owner on startup (obsolete).

---

# Backlog

## Use aiogram-dialog on main bot

## Use i18n to keep two languages: rus and eng

## Clear user onboarding instruction

## Separate main bot 


