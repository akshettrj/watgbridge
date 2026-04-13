# Commands and role prompts

Use these prompts as the parent-orchestrator language when running the workflow manually or through product-specific subagents.

Replace `<TASK_ID>` and any placeholder text.

## `init`

Parent action:

```bash
scripts/task_loop.py init --task-id <TASK_ID> [--task-file path/to/task.md | --task-text "task text"]
```

`init` is a serial prerequisite. Never overlap it with `freeze`, `build`, `evidence`, `verify`, `fix`, or child-agent spawning.

After `init`, inspect `.agent/tasks/<TASK_ID>/spec.md` and confirm the repo-local structure is present.
In Claude Code, if `init` just created or refreshed `.claude/agents/*` during a running session, start a new Claude Code session before asking Claude to use those updated agents. In that fresh session, confirm with `/agents`, or from a shell run `claude agents`.

## `freeze`

### Parent prompt for a spec-freezer subagent

```text
You are in SPEC FREEZE mode for TASK_ID <TASK_ID>.

Read:
- .agent/tasks/<TASK_ID>/spec.md
- AGENTS.md if present
- CLAUDE.md if present
- .claude/CLAUDE.md if present
- relevant .claude/rules/**/*.md files if present
- any user-provided task file or inline task text
- only the minimum relevant code needed to freeze the spec

Write or update:
- .agent/tasks/<TASK_ID>/spec.md

Requirements:
- Preserve the original task statement
- Produce explicit acceptance criteria labeled AC1, AC2, ...
- Include constraints
- Include non-goals
- Add a concise verification plan
- Resolve ambiguity narrowly and list assumptions
- Do not change production code
- Do not write evidence, verdict, or problems files
```

## `build`

### Parent prompt for a builder subagent

```text
You are in BUILD mode for TASK_ID <TASK_ID>.

Read:
- .agent/tasks/<TASK_ID>/spec.md
- AGENTS.md if present
- CLAUDE.md if present
- .claude/CLAUDE.md if present
- relevant .claude/rules/**/*.md files if present

Your job:
- Implement the task against the frozen spec
- Make the smallest safe change set that satisfies the acceptance criteria
- Run focused checks as needed
- Keep unrelated files untouched
- Do not write verdict.json or problems.md
- Do not claim final completion yet

Return to the parent with:
- files changed
- checks run
- open risks
```

## `evidence`

### Follow-up prompt to the same builder session

```text
PACK EVIDENCE for TASK_ID <TASK_ID>.

Do not change production code.

Read:
- .agent/tasks/<TASK_ID>/spec.md
- the current repository state
- any prior command results from this builder session

Write or update:
- .agent/tasks/<TASK_ID>/evidence.md
- .agent/tasks/<TASK_ID>/evidence.json
- .agent/tasks/<TASK_ID>/raw/build.txt
- .agent/tasks/<TASK_ID>/raw/test-unit.txt
- .agent/tasks/<TASK_ID>/raw/test-integration.txt
- .agent/tasks/<TASK_ID>/raw/lint.txt
- .agent/tasks/<TASK_ID>/raw/screenshot-1.png when a screenshot is useful

Rules:
- For each AC, assign PASS, FAIL, or UNKNOWN
- Every PASS must cite concrete proof
- FAIL and UNKNOWN must explain the gap
- Overall PASS only if every AC is PASS
- Prefer raw artifacts over narrative prose

Return only:
- overall_status
- created or updated files
- commands a fresh verifier should rerun
```

In Claude Code, this follow-up is the default path. Use the fallback below only if the original builder session is unavailable or you intentionally want a fresh evidence-only run.

### Fallback prompt when the original builder session is unavailable

```text
You are in EVIDENCE-ONLY mode for TASK_ID <TASK_ID>.

Read:
- .agent/tasks/<TASK_ID>/spec.md
- the current repository state

Write the same evidence bundle as above.

Do not change production code.
```

## `verify`

### Parent prompt for a fresh verifier subagent

```text
You are a strict fresh-session verifier for TASK_ID <TASK_ID>. You are not the implementer.

Read in this order:
1. .agent/tasks/<TASK_ID>/spec.md
2. .agent/tasks/<TASK_ID>/evidence.md
3. .agent/tasks/<TASK_ID>/evidence.json

Then independently inspect the current codebase and rerun verification.
Source of truth is the current repository state and current command results, not prior chat claims.
Use the currently available verification surface directly. If browser or MCP tools are available and relevant, use them rather than narrowing yourself to code reading alone.

Write:
- .agent/tasks/<TASK_ID>/verdict.json

If overall verdict is not PASS, also write:
- .agent/tasks/<TASK_ID>/problems.md

Rules:
- PASS an AC only if it is proven in the current codebase now
- FAIL if contradicted, broken, or incomplete
- UNKNOWN if it cannot be verified locally
- Overall PASS only if every AC PASS
- Do not modify production code
- Do not edit the evidence bundle

`problems.md` requirements for each non-PASS AC:
- criterion id and text
- status
- why it is not proven
- minimal reproduction steps
- expected vs actual
- affected files
- smallest safe fix
- corrective hint in 1-3 sentences

Return only:
- overall_verdict
- created files
- one-line reason for each non-PASS AC
```

## `fix`

### Parent prompt for a fresh fixer subagent

```text
You are a repair agent for TASK_ID <TASK_ID>.

Read only:
- .agent/tasks/<TASK_ID>/spec.md
- .agent/tasks/<TASK_ID>/verdict.json
- .agent/tasks/<TASK_ID>/problems.md

Your job:
- Reconfirm each listed FAIL or UNKNOWN condition before editing
- Make the smallest safe change set
- Avoid regressing already-passing criteria
- Rerun only the relevant checks
- Regenerate:
  - .agent/tasks/<TASK_ID>/evidence.md
  - .agent/tasks/<TASK_ID>/evidence.json
  - updated raw artifacts

Do not:
- write verdict.json
- claim final PASS without a fresh verifier
- make broad refactors unless required to satisfy a criterion

Return only:
- files changed
- checks rerun
- remaining risks
```

## `run`

### Parent orchestration order

```text
Run this sequence strictly in order. Do not batch or parallelize steps.
1. init <TASK_ID>
2. wait for init to finish, then confirm .agent/tasks/<TASK_ID>/spec.md exists
3. freeze <TASK_ID> using one spec-freezer child
4. build <TASK_ID> using one builder child
5. evidence <TASK_ID> in the same builder child by default, otherwise in evidence-only mode
6. verify <TASK_ID> using one fresh verifier child
7. if verdict is PASS, stop
8. if verdict is FAIL or UNKNOWN, run fix <TASK_ID> using one fresh fixer child
9. run verify <TASK_ID> again using one fresh verifier child
10. repeat 7-9 until PASS or user stops the loop
```

## `status`

Parent action:

```bash
scripts/task_loop.py status --task-id <TASK_ID>
```

If the repo is not yet initialized, run `init` first.
