# Branching Strategy

This project follows the GitHub Flow strategy.

```
Strategy: GitHub Flow
Default branch: main (always deployable)

Branch naming:
  feature/<issue-number-or-description>  — new functionality
  fix/<issue-number-or-description>      — bug fixes
  docs/<description>                     — documentation only
  refactor/<description>                 — code restructuring
  test/<description>                     — test additions

Workflow:
  1. Create branch from main
  2. Make changes with conventional commits
  3. Push branch and open Pull Request
  4. Get at least 1 review approval
  5. Squash merge into main
  6. Delete the feature branch

Rules:
  - Never push directly to main
  - All changes via Pull Request
  - PRs require at least 1 review
  - Squash merge preferred (keeps main history clean)
  - Delete branch after merge
  - Keep branches short-lived (< 1 week ideally)
  - Rebase on main if branch falls behind
```
