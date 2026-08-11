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
	runCycle(ctx, cfg, k8sClient, promClient)

	// Loop
	for {
		select {
		case <-ctx.Done():
			slog.Info("Shutting down reconciliation loop")
			server.Shutdown(context.Background())
			return
		case <-ticker.C:
			runCycle(ctx, cfg, k8sClient, promClient)
		}
	}
}

func runCycle(ctx context.Context, cfg *config.Config, k8sClient *k8s.Client, promClient *telemetry.Client) {
	slog.Info("--- Starting Reconciliation Cycle ---")
	startTime := time.Now()

	// Phase 1: Kubernetes Discovery (Temporary test code)
	deps, err := k8sClient.GetDeployments(ctx, cfg.Kubernetes.Namespace)
	if err != nil {
		slog.Error("Failed to get deployments", slog.Any("error", err))
	} else {
		slog.Info("Found Deployments in the cluster.", slog.Int("count", len(deps.Items)))
	}

	// Phase 2: Telemetry Discovery (Temporary test code)
	cpu, err := promClient.GetCPUUsage(ctx, "argocd", "argocd-server", "5m")
	if err != nil {
		slog.Error("Failed to get CPU usage", slog.Any("error", err))
	} else {
		slog.Info("CPU Usage for argocd-server", slog.Float64("cpu_cores", cpu))
	}

	slog.Info("--- Completed Reconciliation Cycle ---", slog.Duration("duration", time.Since(startTime)))
}
