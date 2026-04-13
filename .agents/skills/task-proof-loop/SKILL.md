---
name: repo-task-proof-loop
description: Repo-local workflow skill for large coding tasks. Initializes .agent/tasks/TASK_ID artifacts, installs project-scoped Codex and Claude subagents, updates AGENTS.md plus the repo's Claude guide file with the workflow, and runs a spec-freeze → build → evidence → verify → fix loop with fresh-session verification.
license: Apache-2.0
compatibility: Skills-compatible coding agents. Integrates with Codex and Claude Code project-scoped subagents. Bundled scripts require Python 3.10+.
metadata:
  author: OpenAI
  version: "1.0.0"
---

# Repo Task Proof Loop

Use this skill when the user wants a repeatable, auditable implementation workflow for a non-trivial coding task, especially a feature, refactor, migration, or bug fix that should leave repo-local proof in `.agent/tasks/<TASK_ID>/`.

All task artifacts created by this workflow must stay inside the repository.

When the examples below mention `scripts/task_loop.py`, that path is relative to this skill root. Run it while your shell working directory is inside the target repository.

## What this skill does

1. Initializes a strict repo-local task folder under `.agent/tasks/<TASK_ID>/`
2. Seeds or updates the required artifact files
3. Installs project-scoped Codex and Claude subagent templates into `.codex/agents/` and `.claude/agents/`
4. Updates `AGENTS.md` and the repo's Claude guide file (`CLAUDE.md` or `.claude/CLAUDE.md`) with a managed block that explains the workflow
5. Guides the agent through a strict loop:
   - spec freeze
   - builder implementation
   - evidence packing
   - fresh verification
   - minimal fix
   - fresh verification again until `PASS`

See:
- `references/REFERENCE.md`
- `references/COMMANDS.md`
- `references/SUBAGENTS.md`
- `references/SCHEMAS.md`

## Commands this skill supports

Treat the following words as commands when the user invokes this skill:

- `init <TASK_ID>`: create `.agent/tasks/<TASK_ID>/`, install or refresh subagent templates, and update `AGENTS.md` plus the repo's Claude guide file
- `freeze <TASK_ID>`: create or refine `spec.md` from the user task, task file, and repo guidance
- `build <TASK_ID>`: implement the task against the frozen spec
- `evidence <TASK_ID>`: create or refresh `evidence.md`, `evidence.json`, and raw artifacts without changing production code
- `verify <TASK_ID>`: run a fresh verifier pass and write `verdict.json`, plus `problems.md` when needed
- `fix <TASK_ID>`: apply the smallest safe fix set from `problems.md`, then refresh the evidence bundle
- `run <TASK_ID>`: execute the full loop from spec freeze through verification
- `status <TASK_ID>`: summarize current artifact status

If the user does not supply a command, infer the next step from repo state:
- If the task folder does not exist, do `init` only. Do not start `freeze`, `build`, `evidence`, `verify`, `fix`, or subagent work until `init` succeeds and `.agent/tasks/<TASK_ID>/spec.md` exists.
- If `spec.md` is missing or placeholder-only, do `freeze`
- If implementation is not yet complete, do `build`
- If evidence is stale or missing, do `evidence`
- If no fresh verdict exists, do `verify`
- If verdict is not `PASS`, do `fix`

## Initialization step

Run the bundled initializer from the repository root or current working directory inside the repo:

```bash
scripts/task_loop.py init --task-id <TASK_ID>
```

Optional task seeding:

```bash
scripts/task_loop.py init --task-id <TASK_ID> --task-file path/to/task.md
scripts/task_loop.py init --task-id <TASK_ID> --task-text "User task text"
```

The initializer will:

- resolve the repo root
- create `.agent/tasks/<TASK_ID>/`
- create all required artifacts, including placeholders under `raw/`
- install project-scoped subagent files
- insert or refresh managed workflow blocks in `AGENTS.md` and the repo's Claude guide file

For Claude Code, the initializer keeps its managed workflow block in the repo-root `CLAUDE.md`. Claude Code also supports `.claude/CLAUDE.md`, `.claude/rules/*.md`, and `CLAUDE.local.md`, but this skill treats root `CLAUDE.md` as the primary project guide because Claude surfaces it directly.

In Claude Code, if `init` just wrote or refreshed `.claude/agents/*` during a running session, start a new Claude Code session before expecting those updated agents to appear.

Treat `init` as a serial prerequisite. Never overlap it with `freeze`, `build`, `evidence`, `verify`, `fix`, or child-agent spawning.

## Heavy-task default workflow

For large tasks, prefer subagents when the product supports them.

### Preferred sequence

1. Run `init <TASK_ID>` if needed. Wait for it to finish, then confirm `.agent/tasks/<TASK_ID>/spec.md` and the repo-local task structure exist before continuing.
2. Only after `init` completes, spawn exactly one spec-freezer subagent and wait for it
3. Spawn exactly one builder subagent and let it implement
4. Continue with the same builder session for evidence packing
5. Spawn exactly one fresh verifier subagent and wait for it
6. If verdict is not `PASS`, spawn exactly one fresh fixer subagent
7. Spawn one fresh verifier subagent again
8. Repeat steps 6-7 until the verifier returns `PASS` or the user stops the loop

### Platform behavior

- In Codex, explicitly ask for subagents. Do not assume they spawn automatically.
- In Claude Code, prefer the installed project subagents from `.claude/agents/`. Reuse the same builder child for the evidence step by default. Only run a fresh builder in evidence-only mode if the original builder session is unavailable or you intentionally discarded it. If `init` just refreshed `.claude/agents/*` during a running Claude Code session, start a new session before relying on the updated agents. Use `/agents` to inspect the available Claude agents, and use `claude --agent <name>` only when you intentionally want a single-agent main-thread session instead of the parent-orchestrated proof loop.
- In Claude Code, keep the orchestration flat: the parent session should select each role directly instead of asking one custom task agent to spawn another.
- If subagents are unavailable, preserve the same role separation across separate sessions or clear mode changes in the current session.

Use the exact role prompts from `references/COMMANDS.md`.

## Spec freeze requirements

`spec.md` must contain at least:

- original task statement
- explicit acceptance criteria labeled `AC1`, `AC2`, ...
- constraints
- non-goals

It may also include:

- repo guidance sources
- verification plan
- assumptions resolved narrowly from the user request

Do not edit production code during spec freeze.

## Evidence packing requirements

`evidence.md` and `evidence.json` must judge each acceptance criterion independently with one of:

- `PASS`
- `FAIL`
- `UNKNOWN`

Evidence packing may run missing checks, but it must not keep changing production code.

Every `PASS` must cite concrete proof such as:

- file paths
- commands run
- exit codes
- output summaries
- artifact paths under `raw/`

Do not claim overall `PASS` in the evidence bundle unless every acceptance criterion is `PASS`.

## Fresh verification requirements

The verifier must be a fresh session or fresh subagent.

The verifier must judge the current repository state and current rerun results, not the builder narrative.

The verifier writes:

- `.agent/tasks/<TASK_ID>/verdict.json`
- `.agent/tasks/<TASK_ID>/problems.md` only when overall verdict is not `PASS`

`problems.md` must include, for each non-`PASS` criterion:

- criterion id and text
- status
- why it is not proven
- minimal reproduction steps
- expected vs actual
- affected files
- smallest safe fix
- corrective hint in 1-3 sentences

The verifier must not modify production code or backfill the evidence bundle.

## Fixer requirements

The fixer reads only:

- `spec.md`
- `verdict.json`
- `problems.md`

The fixer must:

- reconfirm each listed problem in the codebase before editing
- make the smallest safe change set
- avoid regressing already-passing criteria
- regenerate `evidence.md`, `evidence.json`, and raw artifacts
- stop without writing final sign-off

## Validation

Before claiming the workflow is correctly initialized or the artifact set is complete, run:

```bash
scripts/task_loop.py validate --task-id <TASK_ID>
```

For a quick summary:

```bash
scripts/task_loop.py status --task-id <TASK_ID>
```

## Guardrails

- Keep `.agent/tasks/<TASK_ID>/` inside the repo
- Never claim task completion unless every acceptance criterion is `PASS`
- Separate evaluator and fixer roles
- Keep the verifier fresh
- Prefer the smallest defensible diffs during fixes
- Preserve existing user guidance outside the managed blocks in `AGENTS.md` and the repo's chosen Claude guide file
