# Current State

Living document of what's built and what's next.

- **Phase**: Phase 1 (Foundation & Data Collection) — completed
- **Next Phase**: Phase 2 (Data Export & Metrics)

## Implemented
- Config system (`config.yaml`, `config.local.yaml`, `os.Getenv`) and structured logging (`slog`)
- Continuous reconciliation loop with graceful shutdown
- Enhanced K8s client with node, namespace, pod, HPA discovery and workload classification
- Enhanced Telemetry client with memory, network tx/rx queries, and parameterized time windows
- Enhanced FinOps client with namespace and cluster cost queries, and parameterized time windows
- All clients integrated into `main.go` reconciliation loop
- Unit tests for all clients and config system

## Known Issues
- `agent.exe` binary committed to repo (should be in `.gitignore`)
- Pod naming assumes standard K8s deployment controller naming scheme
- All connections are plain HTTP without TLS

## Blocked
- Nothing currently blocked

## Branch
- Currently on `feature/phase1-integration`
