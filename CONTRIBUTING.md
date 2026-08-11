# Contributing to Watchdog Agent

First off, thanks for taking the time to contribute!

## Getting Started
1. Clone the repo
2. Branch off of `main`
3. Set up the development environment as described in the README

## Development Workflow
We use the **GitHub Flow** strategy.
- Main is always deployable.
- All new features and fixes should be done in a separate branch.

## Branch Naming Conventions
- `feature/<description>`
- `fix/<description>`
- `docs/<description>`
- `refactor/<description>`
- `test/<description>`

## Commit Message Format
We use Conventional Commits. See the full rules in [commit-conventions.md](.agents/rules/commit-conventions.md).

## Pull Request Process
1. Follow the PR template.
2. See the PR rules in [pr-conventions.md](.agents/rules/pr-conventions.md).
3. At least 1 review approval is required before merging.

## Code Standards
Please read our [Go Standards](.agents/rules/go-standards.md) before submitting code.

## Code Review Checklist
- [ ] Code compiles (`go build ./...`)
- [ ] Tests pass (`go test ./...`)
- [ ] No hardcoded values (uses config.yaml)
- [ ] No secrets or credentials committed
- [ ] Documentation updated if needed
- [ ] CURRENT_STATE.md updated if scope changed

## Running Tests and Linting
- **Tests**: `go test ./...`
- **Linting**: `go vet ./...`

## Team
- mithulpranav24
- Kiruthi001
- Maalika07
- RITHAN-SIVASAMY
