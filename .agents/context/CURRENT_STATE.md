# Current State

Living document of what's built and what's next.

- **Phase**: Phase 3 (AI Reasoning & Forecasting) — completed
- **Next Phase**: Phase 4 (GitOps Deployment)

## Implemented
- Config system (`config.yaml`, `config.local.yaml`, `os.Getenv`) and structured logging (`slog`)
- Continuous reconciliation loop with graceful shutdown
- Enhanced K8s client with node, namespace, pod, HPA discovery, and workload classification
- Enhanced Telemetry client with memory, network tx/rx queries, and parameterized time windows
- Enhanced FinOps client with namespace and cluster cost queries, and parameterized time windows
- Phase 2: Consolidated internal data structures (`ClusterSnapshot`, `NamespaceSnapshot`, `WorkloadSnapshot`)
- Phase 2: Service dependency mapping (matching `Service.Spec.Selector` to `Deployment.Spec.Template.Labels`)
- Phase 2: SQLite data persistence layer via `modernc.org/sqlite`
- Phase 3: Recommendation schema and local policy validator stub (`internal/policy`)
- Phase 3: Go HTTP client (`internal/reasoning`) for AI integration
- Phase 3: Python AI Service scaffolding with FastAPI and Dockerfile
- Phase 3: LangGraph reasoner and basic heuristic forecaster (`ai-service/`)
- All clients integrated into `main.go` reconciliation loop
- Unit tests for all clients, config system, storage system, policy validator, and AI integration
- Python `pytest` suite for the LangGraph reasoner

## Known Issues
- `agent.exe` binary committed to repo (should be in `.gitignore`)
- Pod naming assumes standard K8s deployment controller naming scheme
- All connections are plain HTTP without TLS

## Blocked
- Nothing currently blocked

## Branch
- Currently on `feature/phase2-integration`
