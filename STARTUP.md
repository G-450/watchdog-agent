# Watchdog Agent — Local Development Setup

This guide covers running the agent stack locally **without cluster access**. All you need is Go, Python, and the repo cloned. The full integration test (against a real cluster) is run separately by the cluster owner.

## Prerequisites

| Tool | Version | Install |
|------|---------|---------|
| Go | 1.26+ | https://go.dev/dl |
| Python | 3.11+ | https://python.org/downloads |
| kubectl | any | only needed if you have cluster access |

## 1. Clone

```bash
git clone https://github.com/G-450/watchdog-agent.git
cd watchdog-agent
```

## 2. Config

```bash
cp configs/config.yaml configs/config.local.yaml
```

`configs/config.local.yaml` is gitignored — it's your local override. If you have cluster access and port-forwards running, edit it to point at `http://localhost:9090` (Prometheus) and `http://localhost:9003` (OpenCost). If you don't have cluster access, the agent will warn on connection but still compile and run.

## 3. AI Service

```bash
cd ai-service
python -m venv ../.venv
# Windows:
..\.venv\Scripts\activate
# Linux/macOS:
source ../.venv/bin/activate

pip install -r requirements.txt
python -m uvicorn main:app --host 127.0.0.1 --port 8000 --reload
```

The AI service runs standalone — no cluster needed. Verify it's healthy:

```bash
curl http://localhost:8000/health
```

## 4. Agent

In a separate terminal (from the `watchdog-agent/` root):

```bash
go run ./cmd/agent
```

Without live Prometheus/OpenCost, the agent will log warnings about failed telemetry and cost queries but will still start, serve the API on `:8081`, and process any data it can reach.

## 5. Tests

```bash
# Go unit tests
go test ./...
go vet ./...

# Python tests
cd ai-service
python -m pytest
```

All unit tests run without cluster access. They use static fixtures and stub inputs.

## 6. Windows shortcut (PowerShell 7 + psmux)

If you're on Windows and have [psmux](https://github.com/psmux/psmux) installed, the launcher script starts port-forwards, the AI service, and the agent in one named session:

```powershell
.\scripts\start-backend.ps1          # start
.\scripts\start-backend.ps1 -Stop    # stop
.\scripts\start-backend.ps1 -Recreate  # rebuild session
```

Linux/macOS users: run steps 3 and 4 manually in separate terminals.

## Port map

| Service | Port | Notes |
|---------|------|-------|
| Prometheus | 9090 | port-forward from cluster (cluster access required) |
| OpenCost | 9003 | port-forward from cluster (cluster access required) |
| AI Service | 8000 | runs locally, no cluster needed |
| Agent API | 8081 | runs locally, no cluster needed |
| Dashboard | 3000 | see watchdog-dashboard repo |

## Environment variables

| Variable | Default | Purpose |
|----------|---------|---------|
| `WATCHDOG_PROMETHEUS_URL` | from config.yaml | overrides prometheus.url |
| `WATCHDOG_OPENCOST_URL` | from config.yaml | overrides opencost.url |
| `WATCHDOG_AI_SERVICE_URL` | from config.yaml | overrides ai_service.url |
| `GITHUB_TOKEN` | — | required for Phase 4 GitOps PR generation (PAT with `repo` scope) |

## Phase 4 — working without cluster access

The GitOps PR generator (`internal/gitops/`) takes validated `Recommendation` structs and opens a PR against `infra-manifests`. You can develop and unit-test this entirely offline:

1. Use the static fixture at `testdata/cluster_snapshot_fixture.json` as input.
2. Write unit tests in `internal/gitops/pr_generator_test.go` against static `Recommendation` structs.
3. Set `GITHUB_TOKEN` to a PAT with `repo` scope to test actual PR creation against a fork.
4. Integration testing against the real cluster is done by the cluster owner (mithulpranav24).
