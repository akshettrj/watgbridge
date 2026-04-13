
# Reference

When the examples below mention `scripts/task_loop.py`, that path is relative to this skill root. Run it while your shell working directory is inside the target repository.

This skill is designed to be portable, but the repository-local artifacts and subagent files it creates must stay in the target repository.

## Recommended install locations

### Codex

Project skill:
- `.agents/skills/repo-task-proof-loop/`

Personal skill:
- `$HOME/.agents/skills/repo-task-proof-loop/`

### Claude Code

Project skill:
- `.claude/skills/repo-task-proof-loop/`

Personal skill:
- `~/.claude/skills/repo-task-proof-loop/`

The same skill directory can be reused in either product. The initialization script writes repo-local workflow files into the current repository, not into the skill directory.

Claude Code note:
- This skill manages its workflow block in the project-root `CLAUDE.md`.
- Claude Code also loads `.claude/CLAUDE.md`, `.claude/rules/*.md`, and `CLAUDE.local.md`, but those remain compatible add-ons outside this skill's managed block.

## Repo files created by `init`

```text
.agent/tasks/TASK_ID/
  spec.md
  evidence.md
  evidence.json
  raw/
    build.txt
    test-unit.txt
    test-integration.txt
    lint.txt
    screenshot-1.png
  verdict.json
  problems.md
```

The initializer also creates or refreshes these project-level integration files:

```text
.codex/agents/
  task-spec-freezer.toml
  task-builder.toml
  task-verifier.toml
  task-fixer.toml

.claude/agents/
  task-spec-freezer.md
  task-builder.md
  task-verifier.md
  task-fixer.md
```

And it inserts a managed workflow block into:

- `AGENTS.md`
- one Claude guide file: `CLAUDE.md` or `.claude/CLAUDE.md`

If both Claude guide locations exist, the initializer updates the repo-root `CLAUDE.md` and leaves `.claude/CLAUDE.md` untouched. The managed block is replaced in place on re-run, so user-authored content outside the managed markers is preserved.
In Claude Code, `CLAUDE.md` is the project guide file Claude checks during onboarding. When `--guides auto` is used together with `--install-subagents claude` or `--install-subagents both`, the initializer ensures `CLAUDE.md` exists even if the repo previously only had `AGENTS.md`.

## Commands

### Initialize workflow files

```bash
scripts/task_loop.py init --task-id my-task
```

In Claude Code, if `init` just created or refreshed `.claude/agents/*` during a running session, start a new Claude Code session before expecting those refreshed agents to appear. Use `/agents` to inspect available agents, use `/memory` if you want Claude to open and refine the current memory files, and use `claude --agent <name>` only when you intentionally want a direct single-agent session.

Seed the task from a task file:

```bash
scripts/task_loop.py init --task-id my-task --task-file docs/task.md
```

Seed the task from inline text:

```bash
scripts/task_loop.py init --task-id my-task --task-text "Implement feature X"
```

Control which guide files are created or updated:

```bash
scripts/task_loop.py init --task-id my-task --guides auto
scripts/task_loop.py init --task-id my-task --guides both
scripts/task_loop.py init --task-id my-task --guides agents
scripts/task_loop.py init --task-id my-task --guides claude
scripts/task_loop.py init --task-id my-task --guides none
```

For Claude Code, `--guides auto` updates an existing `CLAUDE.md` or `.claude/CLAUDE.md`. If neither exists and Claude subagents are being installed, it creates `CLAUDE.md`.

`--guides auto` keeps existing guide files up to date, creates both guides when none exist yet, and also creates the product-native guide when you install that product's agents (`CLAUDE.md` for Claude, `AGENTS.md` for Codex).

Control which project subagent sets are installed:

```bash
scripts/task_loop.py init --task-id my-task --install-subagents both
scripts/task_loop.py init --task-id my-task --install-subagents codex
scripts/task_loop.py init --task-id my-task --install-subagents claude
scripts/task_loop.py init --task-id my-task --install-subagents none
```

### Validate the artifact set

```bash
scripts/task_loop.py validate --task-id my-task
```

### Summarize current status

```bash
scripts/task_loop.py status --task-id my-task
```

## Expected working pattern

1. Initialize the task folder
2. Freeze the spec
3. Implement
4. Pack evidence
5. Fresh verify
6. Fix if needed
7. Fresh verify again

For exact prompts to use with child agents, see `references/COMMANDS.md`.

## Notes

- The initializer does not write the final `spec.md` content for you. It creates the strict file structure and seeds the task statement when provided. The actual spec freeze is an agent step.
- `evidence.json` and `verdict.json` are created with valid placeholder content so validation can run immediately after `init`.
- `raw/screenshot-1.png` is created as a tiny placeholder PNG so the required path exists from the start.
- Claude Code also loads `.claude/rules/*.md` and `.claude/CLAUDE.md` as project guidance. The initializer discovers those files when seeding guidance sources for the task.
- After installing or refreshing `.claude/agents/` in a running Claude Code session, use `/agents` or start a fresh session before relying on the new agent list.
- Guidance discovery for seeded task specs includes `AGENTS.md`, root `CLAUDE.md`, `.claude/CLAUDE.md`, and `.claude/rules/**/*.md` when present.
