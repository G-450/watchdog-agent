package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"watchdog-agent/internal/k8s"
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

	// Setup basic HTTP server for health checks.
	http.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("OK"))
	})

	port := ":8080"
	fmt.Printf("Agent listening on port %s (Try accessing /health)\n", port)
	
	if err := http.ListenAndServe(port, nil); err != nil {
		log.Fatalf("Server failed to start: %v", err)
	}
}
