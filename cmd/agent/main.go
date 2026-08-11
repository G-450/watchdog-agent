package main

import (
	"context"
	"log"
	"log/slog"
	"net/http"
	"watchdog-agent/internal/config"
	"watchdog-agent/internal/k8s"
	"watchdog-agent/internal/telemetry"
)

func main() {
	// Load configuration
	cfg, err := config.Load("configs/config.yaml")
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	// Initialize logger
	config.InitLogger(cfg)

	slog.Info("Starting Watchdog Federated Agent...", slog.String("agent", cfg.Agent.Name))

	slog.Info("Initializing Kubernetes Client")
	k8sClient, err := k8s.NewClient(cfg)
	if err != nil {
		slog.Error("Failed to initialize K8s client", slog.Any("error", err), slog.String("hint", "Make sure you have a valid ~/.kube/config or KUBECONFIG set!"))
	} else {
		slog.Info("Successfully connected to Kubernetes!")

		// Attempt to fetch deployments in all namespaces
		deps, err := k8sClient.GetDeployments(context.Background(), cfg.Kubernetes.Namespace)
		if err != nil {
			slog.Error("Failed to get deployments", slog.Any("error", err))
		} else {
			slog.Info("Found Deployments in the cluster.", slog.Int("count", len(deps.Items)))
			for i, d := range deps.Items {
				// Print up to 5 for brevity
				if i >= 5 {
					slog.Info("... and more deployments", slog.Int("remaining", len(deps.Items)-5))
					break
				}
				replicas := int32(0)
				if d.Spec.Replicas != nil {
					replicas = *d.Spec.Replicas
				}
				slog.Info("Deployment info",
					slog.String("namespace", d.Namespace),
					slog.String("deployment", d.Name),
					slog.Int("replicas", int(replicas)),
				)
			}
		}
	}

	// --- PHASE 2 TEST: Prometheus Client ---
	slog.Info("Initializing Prometheus Client")
	// For local testing, we assume Prometheus is port-forwarded to localhost:9090
	promClient, err := telemetry.NewClient(cfg)
	if err != nil {
		slog.Error("Failed to initialize Prometheus client", slog.Any("error", err))
	} else {
		slog.Info("Successfully initialized Prometheus client!")
		// Let's check CPU for the argocd-server deployment as a test
		cpu, err := promClient.GetCPUUsage(context.Background(), "argocd", "argocd-server")
		if err != nil {
			slog.Error("Failed to get CPU usage", slog.Any("error", err), slog.String("hint", "Make sure you are port-forwarding Prometheus: kubectl port-forward svc/prometheus-stack-kube-prom-prometheus -n monitoring 9090:9090"))
		} else {
			slog.Info("CPU Usage for argocd-server", slog.String("cpu_cores", cpu))
		}
	}

	// Setup basic HTTP server for health checks.
	http.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("OK"))
	})

	port := fmt.Sprintf(":%d", cfg.Agent.Port)
	slog.Info("Agent listening", slog.String("port", port), slog.String("hint", "Try accessing /health"))

	if err := http.ListenAndServe(port, nil); err != nil {
		slog.Error("Server failed to start", slog.Any("error", err))
		log.Fatalf("Server failed to start: %v", err)
	}
}
