# Pull Request Conventions

```
PR Title Format: <type>(<scope>): <description>

Examples:
  feat(telemetry): add memory and network PromQL queries
  fix(finops): handle timeout on OpenCost API calls
  refactor(k8s): extract config into separate package

PR Body Template:
  ## What
  Brief description of what this PR does.

  ## Why
  Context and motivation for the change.

  ## How
  Implementation approach and key design decisions.

  ## Testing
  How this was tested (unit tests, manual testing, etc.).

  ## Checklist
  - [ ] Code compiles (`go build ./...`)
  - [ ] Tests pass (`go test ./...`)
  - [ ] No hardcoded values (uses config.yaml)
  - [ ] No secrets or credentials committed
  - [ ] Documentation updated if needed
  - [ ] CURRENT_STATE.md updated if scope changed

Review Rules:
  - At least 1 approval required before merge
  - Reviewer should check: correctness, error handling, test coverage, config usage
  - Use squash merge to keep main history clean
```
