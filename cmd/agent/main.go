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

	"watchdog-agent/internal/api"
	"watchdog-agent/internal/config"
	"watchdog-agent/internal/finops"
	"watchdog-agent/internal/k8s"
	"watchdog-agent/internal/model"
	"watchdog-agent/internal/policy"
	"watchdog-agent/internal/reasoning"
	"watchdog-agent/internal/storage"
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

	// Initialize Storage
	dbPath := cfg.Storage.Path
	if dbPath == "" {
		dbPath = "./data.db" // Fallback default
	}
	store, err := storage.NewSQLiteStore(dbPath)
	if err != nil {
		slog.Error("Failed to initialize storage", slog.Any("error", err))
		log.Fatalf("Failed to initialize storage: %v", err)
	}
	defer store.Close()

	aiClient := reasoning.NewClient(cfg)
	policyValidator := policy.NewLocalValidator()

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

	port := fmt.Sprintf(":%d", cfg.Agent.Port)
	apiServer := api.NewServer(store, cfg.Agent.Name, cfg.API.AllowedOrigins)
	server := &http.Server{Addr: port, Handler: apiServer.Handler(), ReadHeaderTimeout: 5 * time.Second}

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
	runCycle(ctx, cfg, k8sClient, promClient, finopsClient, store, aiClient, policyValidator)

	// Loop
	for {
		select {
		case <-ctx.Done():
			slog.Info("Shutting down reconciliation loop")
			server.Shutdown(context.Background())
			return
		case <-ticker.C:
			runCycle(ctx, cfg, k8sClient, promClient, finopsClient, store, aiClient, policyValidator)
		}
	}
}

func runCycle(ctx context.Context, cfg *config.Config, k8sClient *k8s.Client, promClient *telemetry.Client, finopsClient *finops.Client, store storage.Store, aiClient *reasoning.Client, validator policy.Validator) {
	slog.Info("--- Starting Reconciliation Cycle ---")
	startTime := time.Now()

	clusterSnap := &model.ClusterSnapshot{
		Timestamp:  startTime,
		Namespaces: make(map[string]*model.NamespaceSnapshot),
	}

	nodes, err := k8sClient.GetNodes(ctx)
	if err == nil {
		clusterSnap.Nodes = len(nodes.Items)
	}

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

		services, err := k8sClient.GetServices(ctx, ns.Name)
		if err != nil {
			slog.Warn("Failed to list services", slog.String("namespace", ns.Name), slog.Any("error", err))
		}

		nsCost, _ := finopsClient.GetNamespaceCost(ctx, ns.Name, "5m")
		var nsTotalCost float64
		if nsCost != nil {
			nsTotalCost = nsCost.TotalCost
		}

		nsSnap := &model.NamespaceSnapshot{
			Name:          ns.Name,
			Workloads:     make(map[string]*model.WorkloadSnapshot),
			NamespaceCost: nsTotalCost,
		}

		for _, dep := range deps.Items {
			cpu, _ := promClient.GetCPUUsage(ctx, ns.Name, dep.Name, "5m")
			mem, _ := promClient.GetMemoryUsage(ctx, ns.Name, dep.Name)
			netRx, _ := promClient.GetNetworkReceive(ctx, ns.Name, dep.Name, "5m")
			netTx, _ := promClient.GetNetworkTransmit(ctx, ns.Name, dep.Name, "5m")
			depCost, _ := finopsClient.GetDeploymentCost(ctx, ns.Name, dep.Name, "5m")

			var totalCost float64
			if depCost != nil {
				totalCost = depCost.TotalCost
			}

			// Extract Requests and Limits
			var cpuReq, cpuLim float64
			var memReq, memLim int64
			for _, container := range dep.Spec.Template.Spec.Containers {
				if req := container.Resources.Requests.Cpu(); req != nil {
					cpuReq += float64(req.MilliValue()) / 1000.0
				}
				if lim := container.Resources.Limits.Cpu(); lim != nil {
					cpuLim += float64(lim.MilliValue()) / 1000.0
				}
				if req := container.Resources.Requests.Memory(); req != nil {
					memReq += req.Value()
				}
				if lim := container.Resources.Limits.Memory(); lim != nil {
					memLim += lim.Value()
				}
			}

			var replicas int32
			if dep.Spec.Replicas != nil {
				replicas = *dep.Spec.Replicas
			}

			// Map Services
			var serviceDeps []string
			if services != nil {
				for _, svc := range services.Items {
					if len(svc.Spec.Selector) == 0 {
						continue
					}
					match := true
					for k, v := range svc.Spec.Selector {
						if dep.Spec.Template.Labels[k] != v {
							match = false
							break
						}
					}
					if match {
						serviceDeps = append(serviceDeps, svc.Name)
					}
				}
			}

			wlType := k8sClient.ClassifyWorkload(&dep)

			nsSnap.Workloads[dep.Name] = &model.WorkloadSnapshot{
				Name:                dep.Name,
				Namespace:           ns.Name,
				Type:                string(wlType),
				Replicas:            replicas,
				CPURequests:         cpuReq,
				CPULimits:           cpuLim,
				MemRequests:         memReq,
				MemLimits:           memLim,
				CPUUsage:            cpu,
				MemUsage:            mem,
				NetRxUsage:          netRx,
				NetTxUsage:          netTx,
				TotalCost:           totalCost,
				IsExcluded:          false, // Add specific workload exclusions later if needed
				ServiceDependencies: serviceDeps,
			}
		}

		clusterSnap.Namespaces[ns.Name] = nsSnap
	}

	clusterCost, err := finopsClient.GetClusterCost(ctx, "5m")
	if err == nil && clusterCost != nil {
		clusterSnap.TotalCost = clusterCost.TotalCost
	}

	// Persist snapshot to local storage
	if err := store.SaveSnapshot(ctx, clusterSnap); err != nil {
		slog.Error("Failed to persist cluster snapshot", slog.Any("error", err))
	} else {
		slog.Info("Cluster snapshot persisted successfully")
	}

	// Send to AI Service
	recs, err := aiClient.Analyze(ctx, clusterSnap)
	if err != nil {
		slog.Warn("Failed to get recommendations from AI service", slog.Any("error", err))
	} else {
		for _, rec := range recs {
			err := validator.Validate(rec)
			if err != nil {
				slog.Info("Recommendation rejected by policy",
					slog.String("target", rec.Target),
					slog.String("reason", rec.RejectionReason))
			} else {
				slog.Info("Recommendation approved",
					slog.String("target", rec.Target),
					slog.Float64("savings", rec.ExpectedSavings),
					slog.Float64("confidence", rec.ConfidenceScore))
			}
		}
		if err := store.SaveRecommendations(ctx, recs); err != nil {
			slog.Error("Failed to persist recommendations", slog.Any("error", err))
		}
	}

	slog.Info("--- Completed Reconciliation Cycle ---",
		slog.Duration("duration", time.Since(startTime)),
		slog.Int("namespaces_profiled", len(clusterSnap.Namespaces)),
		slog.Float64("total_cluster_cost", clusterSnap.TotalCost),
	)
}
