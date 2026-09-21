package main

import (
	"context"
	"fmt"
	"log"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"regexp"
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

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
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
	policyValidator := policy.NewFromConfig(cfg.Policy)

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
			shutdownCtx, cancelShutdown := context.WithTimeout(context.Background(), 5*time.Second)
			server.Shutdown(shutdownCtx)
			cancelShutdown()
			return
		case <-ticker.C:
			runCycle(ctx, cfg, k8sClient, promClient, finopsClient, store, aiClient, policyValidator)
		}
	}
}

func runCycle(ctx context.Context, cfg *config.Config, k8sClient *k8s.Client, promClient *telemetry.Client, finopsClient *finops.Client, store storage.Store, aiClient *reasoning.Client, validator policy.Validator) {
	slog.Info("--- Starting Reconciliation Cycle ---")
	startTime := time.Now().UTC()

	clusterSnap := &model.ClusterSnapshot{
		Timestamp:  startTime,
		ClusterID:  cfg.Agent.Name,
		Namespaces: make(map[string]*model.NamespaceSnapshot),
	}

	nodes, err := k8sClient.GetNodes(ctx)
	if err != nil {
		slog.Warn("Failed to list nodes", slog.Any("error", err))
	} else {
		clusterSnap.Nodes = len(nodes.Items)
	}

	namespaces, err := k8sClient.GetNamespaces(ctx)
	if err != nil {
		slog.Error("Failed to list namespaces", slog.Any("error", err))
		return
	}

	// One allocation query per cycle; namespace and workload costs are derived from it.
	allocations, err := finopsClient.GetAllocations(ctx)
	if err != nil {
		slog.Warn("Failed to fetch OpenCost allocations; costs will be zero this cycle", slog.Any("error", err))
		allocations = &finops.Allocations{}
	}

	history := make(map[string]reasoning.WorkloadHistory)
	for _, ns := range namespaces.Items {
		if cfg.Kubernetes.Namespace != "" && ns.Name != cfg.Kubernetes.Namespace {
			continue
		}
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

		nsSnap := &model.NamespaceSnapshot{
			Name:          ns.Name,
			Workloads:     make(map[string]*model.WorkloadSnapshot),
			NamespaceCost: allocations.NamespaceCost(ns.Name),
		}

		for i := range deps.Items {
			dep := &deps.Items[i]
			nsSnap.Workloads[dep.Name] = collectWorkload(ctx, k8sClient, promClient, allocations, dep, services)
			history[reasoning.HistoryKey(ns.Name, dep.Name)] = collectHistory(ctx, promClient, ns.Name, dep.Name)
		}

		clusterSnap.Namespaces[ns.Name] = nsSnap
	}
	clusterSnap.TotalCost = allocations.ClusterCost()

	// Persist snapshot to local storage
	snapshotID, err := store.SaveSnapshot(ctx, clusterSnap)
	if err != nil {
		slog.Error("Failed to persist cluster snapshot", slog.Any("error", err))
		return
	}
	slog.Info("Cluster snapshot persisted successfully", slog.Int64("snapshot_id", snapshotID))

	// Send to AI Service
	recs, err := aiClient.Analyze(ctx, clusterSnap, history)
	if err != nil {
		slog.Warn("Failed to get recommendations from AI service", slog.Any("error", err))
	} else {
		approved := 0
		for _, rec := range recs {
			if err := validator.Validate(rec); err != nil {
				slog.Info("Recommendation rejected by policy",
					slog.String("target", rec.Target),
					slog.String("action", rec.Action),
					slog.String("reason", rec.RejectionReason))
				continue
			}
			approved++
			slog.Info("Recommendation approved",
				slog.String("target", rec.Target),
				slog.String("action", rec.Action),
				slog.Float64("savings", rec.ExpectedSavings),
				slog.Float64("confidence", rec.ConfidenceScore))
		}
		if err := store.SaveRecommendations(ctx, snapshotID, recs); err != nil {
			slog.Error("Failed to persist recommendations", slog.Any("error", err))
		}
		slog.Info("Analysis complete", slog.Int("recommendations", len(recs)), slog.Int("approved", approved))
	}

	slog.Info("--- Completed Reconciliation Cycle ---",
		slog.Duration("duration", time.Since(startTime)),
		slog.Int("namespaces_profiled", len(clusterSnap.Namespaces)),
		slog.Float64("monthly_cluster_cost", clusterSnap.TotalCost),
	)
}

// collectWorkload builds a deployment's snapshot from its spec, telemetry, and cost allocation.
func collectWorkload(ctx context.Context, k8sClient *k8s.Client, promClient *telemetry.Client, allocations *finops.Allocations, dep *appsv1.Deployment, services *corev1.ServiceList) *model.WorkloadSnapshot {
	ns := dep.Namespace
	cpu, err := promClient.GetCPUUsage(ctx, ns, dep.Name, "5m")
	logTelemetryError(err, "cpu", ns, dep.Name)
	mem, err := promClient.GetMemoryUsage(ctx, ns, dep.Name)
	logTelemetryError(err, "memory", ns, dep.Name)
	netRx, err := promClient.GetNetworkReceive(ctx, ns, dep.Name, "5m")
	logTelemetryError(err, "network receive", ns, dep.Name)
	netTx, err := promClient.GetNetworkTransmit(ctx, ns, dep.Name, "5m")
	logTelemetryError(err, "network transmit", ns, dep.Name)

	podPattern := regexp.MustCompile("^" + telemetry.DeploymentPodPattern(dep.Name) + "$")

	// Extract per-replica requests and limits
	var cpuReq, cpuLim float64
	var memReq, memLim int64
	for _, container := range dep.Spec.Template.Spec.Containers {
		cpuReq += float64(container.Resources.Requests.Cpu().MilliValue()) / 1000.0
		cpuLim += float64(container.Resources.Limits.Cpu().MilliValue()) / 1000.0
		memReq += container.Resources.Requests.Memory().Value()
		memLim += container.Resources.Limits.Memory().Value()
	}

	var replicas int32 = 1 // Kubernetes defaults an unset replica count to 1
	if dep.Spec.Replicas != nil {
		replicas = *dep.Spec.Replicas
	}

	// Map Services whose selector matches the pod template
	serviceDeps := []string{}
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

	workload := &model.WorkloadSnapshot{
		Name:                dep.Name,
		Namespace:           ns,
		Type:                string(k8sClient.ClassifyWorkload(dep)),
		Replicas:            replicas,
		CPURequests:         cpuReq,
		CPULimits:           cpuLim,
		MemRequests:         memReq,
		MemLimits:           memLim,
		CPUUsage:            cpu,
		MemUsage:            mem,
		NetRxUsage:          netRx,
		NetTxUsage:          netTx,
		TotalCost:           allocations.WorkloadCost(ns, podPattern.MatchString),
		ServiceDependencies: serviceDeps,
	}
	if k8s.IsWorkloadExcluded(dep.Annotations) {
		workload.IsExcluded = true
		workload.ExcludeReason = "Opted out with the " + k8s.ExcludeAnnotation + " annotation"
	}
	return workload
}

// collectHistory fetches the usage series the forecaster uses; missing history degrades to a point estimate.
func collectHistory(ctx context.Context, promClient *telemetry.Client, namespace, name string) reasoning.WorkloadHistory {
	cpu, err := promClient.GetCPUUsageHistory(ctx, namespace, name, "5m")
	logTelemetryError(err, "cpu history", namespace, name)
	mem, err := promClient.GetMemoryUsageHistory(ctx, namespace, name)
	logTelemetryError(err, "memory history", namespace, name)
	return reasoning.WorkloadHistory{CPU: cpu, Memory: mem}
}

func logTelemetryError(err error, metric, namespace, name string) {
	if err != nil {
		slog.Warn("Failed to query telemetry", slog.String("metric", metric),
			slog.String("namespace", namespace), slog.String("workload", name), slog.Any("error", err))
	}
}
