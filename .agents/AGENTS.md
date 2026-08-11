# Watchdog Agent AI Context

This repository (`watchdog-agent`) contains the core Go agent for the Watchdog project.

**Project Identity**: Watchdog is an AI-driven FinOps Control Plane for multi-cluster Kubernetes environments.
**Polyrepo Layout**:
- `watchdog-agent` (this repository, Apache 2.0) - Core Go agent running inside Kubernetes clusters.
- `watchdog-infra` (ArgoCD manifests) - Infrastructure definitions.
- `watchdog-docs` (documentation, private, CC BY 2.0) - Project documentation.

## Context Files
- [PROJECT_CONTEXT.md](context/PROJECT_CONTEXT.md)
- [ARCHITECTURE.md](context/ARCHITECTURE.md)
- [CURRENT_STATE.md](context/CURRENT_STATE.md)
- [GLOSSARY.md](context/GLOSSARY.md)

## Rules and Standards
- [Commit Conventions](rules/commit-conventions.md)
- [Branching Strategy](rules/branching-strategy.md)
- [Go Standards](rules/go-standards.md)
- [PR Conventions](rules/pr-conventions.md)

## CRITICAL RULES
- **Never modify resources in kube-system namespace**
- **Never commit secrets, credentials, kubeconfig files, or .env files**
- **Never commit compiled binaries (*.exe, agent binary)**
- **Never push directly to main**
- **Always read CURRENT_STATE.md before starting work**
- **Always update CURRENT_STATE.md after completing work**
- **Use config.yaml for all configurable values, never hardcode**
- **All code must compile before creating a PR**
- **Run `go vet` and `go test ./...` before creating a PR**
