# Glossary

Domain terminology reference for Watchdog:

- **FinOps**: Financial Operations in cloud computing, maximizing business value.
- **HPA / VPA**: Horizontal Pod Autoscaler / Vertical Pod Autoscaler.
- **PDB**: PodDisruptionBudget, ensures a minimum number or percentage of pods remain running.
- **GitOps**: Operational framework that takes DevOps best practices used for application development such as version control, collaboration, compliance, and CI/CD, and applies them to infrastructure automation.
- **ArgoCD**: Declarative, GitOps continuous delivery tool for Kubernetes.
- **OpenCost**: Open source FinOps project for Kubernetes cost monitoring.
- **Prometheus / PromQL**: Systems monitoring and alerting toolkit / Query language for Prometheus.
- **Shadow Mode**: Observe only mode, <70% confidence. No actions are taken.
- **Advisory Mode**: Creates PRs for human approval, 70-95% confidence.
- **Autonomous Mode**: Auto-merges and applies optimizations, >95% confidence.
- **Confidence Score**: The AI's certainty in its optimization recommendation.
- **Forecast Horizon**: The future time period for which resources are predicted.
- **Optimization Window**: The time frame where optimizations can be safely applied.
- **Workload Stabilization Cooldown**: Time waited after an update before another can be applied.
- **Optimization Hysteresis**: Threshold to prevent flip-flopping optimizations.
- **Federated Agent**: Local agent running in each cluster doing data collection and local processing.
- **Global Control Plane**: Centralized system coordinating multiple federated agents.
- **Quarantine Mode**: Isolation state for anomalous or unsafe workloads.
- **SRE / SLO / SLA**: Site Reliability Engineering / Service Level Objective / Service Level Agreement.
- **OPA / Kyverno / Gatekeeper**: Open Policy Agent and alternatives for Kubernetes policy management.
- **Progressive Delivery**: Releasing updates in a controlled, gradual manner.
- **Canary Deployment**: Releasing a new version to a small subset of users before full rollout.
- **Rolling Restart**: Systematically restarting pods in a deployment to apply updates without downtime.
- **Semantic Forward-Fix**: Watchdog's rollback strategy where fixes are pushed forward rather than reversing git history.
- **ADR**: Architecture Decision Record.
- **kube-prometheus-stack**: Collection of Kubernetes manifests, Grafana dashboards, and Prometheus rules.
- **kube-state-metrics**: Service that listens to the Kubernetes API server and generates metrics about the state of the objects.
- **TimescaleDB / pgvector**: Time-series SQL database / vector extension for PostgreSQL.
- **LangGraph**: Framework for building stateful, multi-actor applications with LLMs.
- **PatchTST / LSTM**: AI/ML models for time-series forecasting.
