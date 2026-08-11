# Current State

Living document of what's built and what's next.

- **Phase**: Phase 0 (Project Organization) — completed
- **Next Phase**: Phase 1 (Foundation & Data Collection) — Aug 16-29

## Implemented
- K8s client with in-cluster/out-of-cluster fallback (`internal/k8s`)
- Prometheus telemetry client for CPU usage PromQL (`internal/telemetry`)
- OpenCost finops client for compute cost allocation (`internal/finops`)
- Diagnostic `main.go` that tests all 3 clients + health server on `:8081`
- ArgoCD Application manifests for Prometheus Stack and OpenCost

## Known Issues
- Hardcoded localhost URLs for Prometheus and OpenCost (fix: `config.yaml` in Phase 1)
- Hardcoded test target "argocd-server" (fix: `config.yaml`)
- No reconciliation loop (runs once then hangs)
- No structured logging
- No unit tests
- `agent.exe` binary committed to repo (should be in `.gitignore`)
- Pod naming assumes standard K8s deployment controller naming scheme
- All connections are plain HTTP without TLS

## Blocked
- Nothing currently blocked

## Branch
- Currently on `feature/opencost-client` (not yet merged to main)
