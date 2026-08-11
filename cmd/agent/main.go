package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"watchdog-agent/internal/config"
	"watchdog-agent/internal/k8s"
	"watchdog-agent/internal/telemetry"
)

func main() {
	fmt.Println("Starting Watchdog Federated Agent...")

	// Load configuration
	cfg, err := config.Load("configs/config.yaml")
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}
	fmt.Printf("Loaded config for agent: %s\n", cfg.Agent.Name)

	// --- PHASE 1 TEST: Kubernetes Client ---
	fmt.Println("\n--- Initializing Kubernetes Client ---")
	k8sClient, err := k8s.NewClient(cfg)
	if err != nil {
		fmt.Printf("Failed to initialize K8s client: %v\n", err)
		fmt.Println("Make sure you have a valid ~/.kube/config or KUBECONFIG set!")
	} else {
		fmt.Println("Successfully connected to Kubernetes!")

		// Attempt to fetch deployments in all namespaces
		deps, err := k8sClient.GetDeployments(context.Background(), cfg.Kubernetes.Namespace)
		if err != nil {
			fmt.Printf("Failed to get deployments: %v\n", err)
		} else {
			fmt.Printf("Found %d Deployments in the cluster.\n", len(deps.Items))
			for i, d := range deps.Items {
				// Print up to 5 for brevity
				if i >= 5 {
					fmt.Printf("... and %d more\n", len(deps.Items)-5)
					break
				}
				replicas := int32(0)
				if d.Spec.Replicas != nil {
					replicas = *d.Spec.Replicas
				}
				fmt.Printf(" - Namespace: %s | Deployment: %s | Replicas: %d\n", d.Namespace, d.Name, replicas)
			}
		}
	}
	fmt.Println("----------------------------------------\n")

	// --- PHASE 2 TEST: Prometheus Client ---
	fmt.Println("\n--- Initializing Prometheus Client ---")
	// For local testing, we assume Prometheus is port-forwarded to localhost:9090
	promClient, err := telemetry.NewClient(cfg)
	if err != nil {
		fmt.Printf("Failed to initialize Prometheus client: %v\n", err)
	} else {
		fmt.Println("Successfully initialized Prometheus client!")
		// Let's check CPU for the argocd-server deployment as a test
		cpu, err := promClient.GetCPUUsage(context.Background(), "argocd", "argocd-server")
		if err != nil {
			fmt.Printf("Failed to get CPU usage: %v\n", err)
			fmt.Println("Make sure you are port-forwarding Prometheus:\n  kubectl port-forward svc/prometheus-stack-kube-prom-prometheus -n monitoring 9090:9090")
		} else {
			fmt.Printf("CPU Usage for argocd-server: %s cores\n", cpu)
		}
	}
	fmt.Println("----------------------------------------\n")

	// Setup basic HTTP server for health checks.
	http.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("OK"))
	})

	port := ":8081"
	fmt.Printf("Agent listening on port %s (Try accessing /health)\n", port)

	if err := http.ListenAndServe(port, nil); err != nil {
		log.Fatalf("Server failed to start: %v", err)
	}
}
