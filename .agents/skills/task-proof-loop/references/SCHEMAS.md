# Artifact schemas

These are the required files for each task folder:

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

## `evidence.json`

Required top-level keys:

- `task_id`
- `overall_status`
- `acceptance_criteria`
- `changed_files`
- `commands_for_fresh_verifier`
- `known_gaps`

Allowed status values:

- `PASS`
- `FAIL`
- `UNKNOWN`

Recommended shape:

```json
{
  "task_id": "my-task",
  "overall_status": "UNKNOWN",
  "acceptance_criteria": [
    {
      "id": "AC1",
      "text": "Describe the criterion",
      "status": "UNKNOWN",
      "proof": [
        {
          "type": "command",
          "path": ".agent/tasks/my-task/raw/test-unit.txt",
          "command": "npm test -- --runInBand",
          "exit_code": 0,
          "summary": "Targeted unit tests passed."
        }
      ],
      "gaps": []
    }
  ],
  "changed_files": [],
  "commands_for_fresh_verifier": [],
  "known_gaps": []
}
```

## `verdict.json`

Required top-level keys:

- `task_id`
- `overall_verdict`
- `criteria`
- `commands_run`
- `artifacts_used`

Allowed status values:

- `PASS`
- `FAIL`
- `UNKNOWN`

Recommended shape:

```json
{
  "task_id": "my-task",
  "overall_verdict": "UNKNOWN",
  "criteria": [
    {
      "id": "AC1",
      "status": "UNKNOWN",
      "reason": "Not yet verified."
    }
  ],
  "commands_run": [],
  "artifacts_used": []
}
```

## `problems.md`

Required sections for each non-`PASS` criterion:

- criterion id and text
- status
- why it is not proven
- minimal reproduction steps
- expected vs actual
- affected files
- smallest safe fix
- corrective hint in 1-3 sentences

## Validation script

Run:

```bash
scripts/task_loop.py validate --task-id <TASK_ID>
```

This checks:

- required file presence
- JSON parseability
- top-level key presence
- allowed status values
- task id consistency
