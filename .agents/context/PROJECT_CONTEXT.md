# Project Context

High-level project context for Watchdog AI agents:

- **Identity**: Watchdog is an AI-driven FinOps Control Plane for multi-cluster Kubernetes.
- **Mission**: It observes K8s infrastructure, understands workload behavior, predicts resource requirements, evaluates risks, and executes optimizations via GitOps.
- **Operational Modes**: 
  - *Shadow* (observe only, <70% confidence)
  - *Advisory* (create PRs for human approval, 70-95%)
  - *Autonomous* (auto-merge, >95%)
- **Architecture**: Federated architecture with local agents per cluster, and a lightweight global coordinator.
- **Target Users**: Platform Engineers, SREs, DevOps, FinOps teams.
- **Tech Stack**: Go (agent), Python (AI/ML), Prometheus, OpenCost, ArgoCD, OPA/Kyverno, Kafka, PostgreSQL/TimescaleDB, LangGraph.
- **Polyrepo Layout**: 
  - `watchdog-agent` (Go, Apache 2.0)
  - `watchdog-infra` (manifests)
  - `watchdog-docs` (documentation, CC BY 2.0, private)
- **Team**: mithulpranav24, Kiruthi001, Maalika07, RITHAN-SIVASAMY
- **Deadline**: October 1, 2026 (Semester 5 Cloud Computing project)
- **Cluster**: 2 local Ubuntu Server VMs running ArgoCD
