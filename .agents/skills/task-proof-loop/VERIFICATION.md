# Verification

This package was smoke-tested before packaging.

## Command run

```bash
python scripts/verify_package.py
```

## What the smoke test checks

- `SKILL.md` frontmatter exists and the `name` matches the parent directory
- the skill body is non-empty
- `scripts/task_loop.py init --task-id demo-task --task-text "Implement a demo task."` succeeds inside a fresh temporary git repository
- `scripts/task_loop.py validate --task-id demo-task` returns `valid: true`
- the expected repo-local artifacts are created under `.agent/tasks/demo-task/`
- project-scoped subagent files are created under `.codex/agents/` and `.claude/agents/`
- `AGENTS.md` and `CLAUDE.md` are created with managed workflow blocks
- `--guides auto --install-subagents claude` creates `CLAUDE.md` even if the repo previously only had `AGENTS.md`
- `--guides auto --install-subagents codex` creates `AGENTS.md` even if the repo previously only had `CLAUDE.md`

## Last local result

PASS
