package main

import (
	"context"
	"fmt"
	"log"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"watchdog-agent/internal/config"
	"watchdog-agent/internal/finops"
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

	// Initialize Clients
	k8sClient, err := k8s.NewClient(cfg)
	if err != nil {
		slog.Error("Failed to initialize K8s client", slog.Any("error", err))
		log.Fatalf("Failed to initialize K8s client: %v", err)
	}

	promClient, err := telemetry.NewClient(cfg)
	if err != nil {
		slog.Error("Failed to initialize Prometheus client", slog.Any("error", err))
		log.Fatalf("Failed to initialize Prometheus client: %v", err)
	}

	finopsClient := finops.NewClient(cfg)

	// Setup Graceful Shutdown
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	go func() {
		sig := <-sigCh
		slog.Info("Received signal, shutting down gracefully", slog.String("signal", sig.String()))
		cancel()
	}()

	// Setup Health Server
	http.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("OK"))
	})

	port := fmt.Sprintf(":%d", cfg.Agent.Port)
	server := &http.Server{Addr: port}

	go func() {
		slog.Info("Agent health server listening", slog.String("port", port))
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("Server failed", slog.Any("error", err))
			log.Fatalf("Server failed: %v", err)
		}
	}()

	// Setup Reconciliation Loop
	interval, err := time.ParseDuration(cfg.Agent.ReconcileInterval)
	if err != nil {
		slog.Warn("Invalid reconcile_interval, defaulting to 60s", slog.Any("error", err))
		interval = 60 * time.Second
	}

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	// Run first cycle immediately
	runCycle(ctx, cfg, k8sClient, promClient, finopsClient)

	// Loop
	for {
		select {
		case <-ctx.Done():
			slog.Info("Shutting down reconciliation loop")
			server.Shutdown(context.Background())
			return
		case <-ticker.C:
			runCycle(ctx, cfg, k8sClient, promClient, finopsClient)
		}
	}
}

func runCycle(ctx context.Context, cfg *config.Config, k8sClient *k8s.Client, promClient *telemetry.Client, finopsClient *finops.Client) {
	slog.Info("--- Starting Reconciliation Cycle ---")
	startTime := time.Now()

	namespaces, err := k8sClient.GetNamespaces(ctx)
	if err != nil {
		slog.Error("Failed to list namespaces", slog.Any("error", err))
		return
	}

	for _, ns := range namespaces.Items {
		if k8sClient.IsExcluded(ns.Name, ns.Annotations) {
			slog.Debug("Skipping excluded namespace", slog.String("namespace", ns.Name))
			continue
		}

		deps, err := k8sClient.GetDeployments(ctx, ns.Name)
		if err != nil {
			slog.Error("Failed to list deployments", slog.String("namespace", ns.Name), slog.Any("error", err))
			continue
		}

		nsCost, err := finopsClient.GetNamespaceCost(ctx, ns.Name, "5m")
		if err != nil {
			slog.Debug("Failed to get namespace cost", slog.String("namespace", ns.Name), slog.Any("error", err))
		}

		slog.Info("Namespace Summary",
			slog.String("namespace", ns.Name),
			slog.Int("deployments", len(deps.Items)),
			slog.Any("cost", nsCost),
		)

		for _, dep := range deps.Items {
			cpu, _ := promClient.GetCPUUsage(ctx, ns.Name, dep.Name, "5m")
			mem, _ := promClient.GetMemoryUsage(ctx, ns.Name, dep.Name)
			netRx, _ := promClient.GetNetworkReceive(ctx, ns.Name, dep.Name, "5m")
			netTx, _ := promClient.GetNetworkTransmit(ctx, ns.Name, dep.Name, "5m")
			depCost, _ := finopsClient.GetDeploymentCost(ctx, ns.Name, dep.Name, "5m")

			slog.Info("Workload metrics collected",
				slog.String("namespace", ns.Name),
				slog.String("deployment", dep.Name),
				slog.Float64("cpu_cores", cpu),
				slog.Float64("memory_bytes", mem),
				slog.Float64("net_rx_bytes", netRx),
				slog.Float64("net_tx_bytes", netTx),
				slog.Any("cost", depCost),
			)
		}
	}

	clusterCost, err := finopsClient.GetClusterCost(ctx, "5m")
	if err == nil {
		slog.Info("Cluster Cost Summary", slog.Any("cluster_cost", clusterCost))
	}

	slog.Info("--- Completed Reconciliation Cycle ---", slog.Duration("duration", time.Since(startTime)))
}
