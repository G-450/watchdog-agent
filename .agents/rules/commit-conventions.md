# Commit Conventions

This project uses the Conventional Commits format.

```
Format: <type>(<scope>): <description>

Types: feat, fix, refactor, docs, test, chore, ci
Scope: k8s, telemetry, finops, config, policy, reasoning, gitops, scheduler, api

Examples:
  feat(telemetry): add memory usage PromQL query
  fix(finops): handle empty OpenCost response
  docs: update CURRENT_STATE after Phase 1
  test(k8s): add unit tests for deployment listing
  chore(config): add config.yaml template
  ci: add GitHub Actions lint workflow

Rules:
  - Present tense, imperative mood ("add" not "added")
  - Max 72 chars for subject line
  - Body explains WHY, not WHAT (the diff shows what)
  - Reference issue numbers where applicable: closes #42
  - Empty line between subject and body
  - Multi-line body wrapped at 72 chars
```
