package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"watchdog-agent/internal/finops"
	"watchdog-agent/internal/k8s"
	"watchdog-agent/internal/telemetry"
)

func main() {
	fmt.Println("Starting Watchdog Federated Agent...")

	// --- PHASE 1 TEST: Kubernetes Client ---
	fmt.Println("\n--- Initializing Kubernetes Client ---")
	k8sClient, err := k8s.NewClient()
	if err != nil {
		fmt.Printf("Failed to initialize K8s client: %v\n", err)
		fmt.Println("Make sure you have a valid ~/.kube/config or KUBECONFIG set!")
	} else {
		fmt.Println("Successfully connected to Kubernetes!")

		// Attempt to fetch deployments in all namespaces
		deps, err := k8sClient.GetDeployments(context.Background(), "")
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
	promClient, err := telemetry.NewClient("http://localhost:9090")
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

	// --- PHASE 3 TEST: OpenCost Client ---
	fmt.Println("\n--- Initializing OpenCost Client ---")
	// For local testing, we assume OpenCost is port-forwarded to localhost:9003
	costClient := finops.NewClient("http://localhost:9003")

	fmt.Println("Successfully initialized OpenCost client!")
	// Let's check the cost for the argocd-server deployment over the last 1 day
	alloc, err := costClient.GetDeploymentCost(context.Background(), "argocd", "argocd-server", "1d")
	if err != nil {
		fmt.Printf("Failed to get deployment cost: %v\n", err)
		fmt.Println("Make sure you are port-forwarding OpenCost:\n  kubectl port-forward svc/opencost -n opencost 9003:9003")
	} else {
		fmt.Printf("Cost for argocd-server over the last 1d:\n - Total: $%.4f\n - CPU: $%.4f\n - RAM: $%.4f\n", alloc.TotalCost, alloc.CPUCost, alloc.RAMCost)
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
