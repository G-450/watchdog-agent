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
| `WATCHDOG_GITOPS_ENABLED` | `false` | overrides gitops.enabled |
| `WATCHDOG_GITOPS_REPO` | `mithulpranav24/watchdog-infra` | overrides gitops.repo |
| `GITHUB_TOKEN` | — | required when GitOps is enabled: fine-grained PAT for the target repo with Contents and Pull requests read/write |

The agent reads these from the process environment; nothing loads a `.env` file. Set them in your shell (`$env:GITHUB_TOKEN = "..."` in PowerShell, `export GITHUB_TOKEN=...` in bash). `.env.example` lists them for reference.

## Phase 4 — working without cluster access

The GitOps PR generator (`internal/gitops/`) takes approved `Recommendation`s and opens a PR against `watchdog-infra`, where workload manifests live under `workloads/<namespace>/<name>/`. It keeps one branch and PR per workload (`watchdog/<namespace>-<name>`), changes only the lines whose values change, and does nothing while an open PR already proposes the same change. It is off unless `gitops.enabled` is `true` and `GITHUB_TOKEN` is set. You can develop and unit-test this entirely offline:

1. Inputs are in `testdata/`: a real snapshot, the real recommendations produced for it, and a copy of the `workloads/` tree. `testdata/README.md` explains what each file covers.
2. Unit tests must not touch the network. Seed a local repository from `testdata/manifests/` in `t.TempDir()` and serve the GitHub API from `httptest.Server`.
3. Never run a test with a real `GITHUB_TOKEN` against `watchdog-infra`. For a manual end-to-end check, use a fork.
4. Integration testing against the real cluster is done by the cluster owner (mithulpranav24).
