# Feature backlog

Items below are **not scheduled**; they are stricter or more operational options around SQLite encryption and secrets.

## SQLite / SQLCipher — stricter operations

- **Master key rotation without data loss**: support re-wrapping DBs with a new master (or new derived keys) without full reset; likely an offline admin command and clear backup/rollback story.
- **External KMS / secret store**: source the master material from Vault, cloud KMS, or similar instead of (or in addition to) `WATG_SQLITE_MASTER_KEY` in plain environment.
- **Stronger key isolation (single mode)**: use distinct HKDF info strings (and thus keys) for the GORM bridge DB vs the whatsmeow session store when both are SQLite, instead of a single `watgbridge-v1/single` derivation for both.
- **Operational runbooks**: documented steps for backup, restore, verifying encryption (`sqlcipher`/pragma checks), and disaster recovery when the master secret is lost.

## Notes

- Today, enabling encryption on **existing plaintext** SQLite files is a breaking change until databases are recreated or a future migration tool exists.
