# Watchdog Agent

![Go Version](https://img.shields.io/badge/go-1.26+-blue.svg)
![License](https://img.shields.io/badge/license-Apache%202.0-blue.svg)
![Build](https://img.shields.io/badge/build-passing-brightgreen.svg)

The core federated agent of the Watchdog FinOps Control Plane. Written in Go, it runs inside Kubernetes clusters to collect telemetry, cost data, and workload metadata for AI-driven optimization.

## Architecture
- **K8s Client**: Discovers and monitors deployments and workloads.
- **Telemetry**: Queries Prometheus for usage metrics.
- **FinOps**: Integrates with OpenCost for cost allocation.
- **Agent Loop**: Periodically collects data and reports to the control plane.
- **Visibility API**: Exposes read-only cluster, workload, cost, recommendation, and freshness data for the dashboard.

## Quick Start

1. **Prerequisites**: Go 1.26+, `kubectl`, access to a Kubernetes cluster
2. **Clone**: 
   ```bash
   git clone https://github.com/G-450/watchdog-agent.git
   cd watchdog-agent
   ```
3. **Port-forward Prometheus and OpenCost**:
   ```bash
   kubectl port-forward svc/prometheus-stack-kube-prom-prometheus -n monitoring 9090:9090
   kubectl port-forward svc/opencost -n opencost 9003:9003
   ```
4. **Copy config**: 
   ```bash
   cp configs/config.yaml configs/config.local.yaml
   ```
5. **Run**: 
   ```bash
   go run cmd/agent/main.go
   ```

## Project Structure
```text
├── cmd/
│   └── agent/          # Main entrypoints
├── configs/            # Configuration files
├── internal/           # Private packages
│   ├── k8s/            # Kubernetes client
│   ├── telemetry/      # Prometheus client
│   └── finops/         # OpenCost client
├── .agents/            # AI Agent context and rules (local only)
└── README.md
```

## Configuration
See [configs/config.yaml](configs/config.yaml) for the default configuration values. 

## Dashboard API

The agent serves the following read-only endpoints on its configured port:

- `/health`
- `/api/v1/status`
- `/api/v1/overview`
- `/api/v1/snapshots`
- `/api/v1/workloads`
- `/api/v1/recommendations`

Browser origins must be explicitly listed under `api.allowed_origins`.

## Contributing
Please see `CONTRIBUTING.md` (local only) for details on our development process, branching strategy, and code conventions.

## Related Repos
- [watchdog-infra](https://github.com/mithulpranav24/watchdog-infra)
- [watchdog-docs](https://github.com/mithulpranav24/watchdog-docs) (private)
- `watchdog-dashboard` (operations dashboard)

## Team
- mithulpranav24
- Kiruthi001
- Maalika07
- RITHAN-SIVASAMY

## License
Apache 2.0
