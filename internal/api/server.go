// Package api exposes read-only dashboard endpoints for Watchdog state.
package api

import (
	"encoding/json"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"watchdog-agent/internal/model"
	"watchdog-agent/internal/storage"
)

// Server serves the dashboard visibility API.
type Server struct {
	store          storage.Store
	agentName      string
	startedAt      time.Time
	allowedOrigins map[string]struct{}
}

// NewServer creates a visibility API server.
func NewServer(store storage.Store, agentName string, allowedOrigins []string) *Server {
	origins := make(map[string]struct{}, len(allowedOrigins))
	for _, origin := range allowedOrigins {
		origins[strings.TrimRight(origin, "/")] = struct{}{}
	}
	return &Server{store: store, agentName: agentName, startedAt: time.Now().UTC(), allowedOrigins: origins}
}

// Handler returns the API HTTP handler.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", s.health)
	mux.HandleFunc("GET /api/v1/status", s.status)
	mux.HandleFunc("GET /api/v1/overview", s.overview)
	mux.HandleFunc("GET /api/v1/snapshots", s.snapshots)
	mux.HandleFunc("GET /api/v1/workloads", s.workloads)
	mux.HandleFunc("GET /api/v1/workloads/{namespace}/{name}", s.workload)
	mux.HandleFunc("GET /api/v1/recommendations", s.recommendations)
	return s.withCORS(mux)
}

func (s *Server) health(w http.ResponseWriter, _ *http.Request) {
	s.writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) status(w http.ResponseWriter, r *http.Request) {
	snapshot, err := s.store.GetLatestSnapshot(r.Context())
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, "storage_unavailable", err.Error())
		return
	}
	var lastSnapshot *time.Time
	if snapshot != nil {
		lastSnapshot = &snapshot.Timestamp
	}
	s.writeJSON(w, http.StatusOK, map[string]interface{}{
		"agent_name": s.agentName, "status": "running", "started_at": s.startedAt,
		"last_snapshot_at": lastSnapshot, "storage": "connected",
	})
}

func (s *Server) overview(w http.ResponseWriter, r *http.Request) {
	snapshot, err := s.store.GetLatestSnapshot(r.Context())
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, "storage_unavailable", err.Error())
		return
	}
	if snapshot == nil {
		s.writeError(w, http.StatusNotFound, "no_snapshots", "no cluster snapshots have been collected")
		return
	}
	recommendations, err := s.store.GetRecommendations(r.Context(), storage.RecommendationQuery{LatestOnly: true, Limit: 500})
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, "storage_unavailable", err.Error())
		return
	}
	workloadCount := 0
	potentialSavings := 0.0
	counts := map[string]int{"approved": 0, "rejected": 0, "pending": 0}
	for _, namespace := range snapshot.Namespaces {
		workloadCount += len(namespace.Workloads)
	}
	for _, recommendation := range recommendations {
		status := strings.ToLower(recommendation.Status)
		// Only changes that passed policy count as achievable savings.
		if status == "approved" {
			potentialSavings += recommendation.ExpectedSavings
		}
		counts[status]++
	}
	s.writeJSON(w, http.StatusOK, map[string]interface{}{
		"timestamp": snapshot.Timestamp, "nodes": snapshot.Nodes,
		"namespaces": len(snapshot.Namespaces), "workloads": workloadCount,
		"total_cost": snapshot.TotalCost, "potential_savings": potentialSavings,
		"recommendations": counts,
	})
}

func (s *Server) snapshots(w http.ResponseWriter, r *http.Request) {
	since := time.Now().UTC().Add(-24 * time.Hour)
	if value := r.URL.Query().Get("since"); value != "" {
		parsed, err := time.Parse(time.RFC3339, value)
		if err != nil {
			s.writeError(w, http.StatusBadRequest, "invalid_since", "since must be an RFC3339 timestamp")
			return
		}
		since = parsed
	}
	summaries, err := s.store.GetSnapshotSummaries(r.Context(), since, parseLimit(r, 100))
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, "storage_unavailable", err.Error())
		return
	}
	s.writeJSON(w, http.StatusOK, map[string]interface{}{"items": summaries, "count": len(summaries)})
}

func (s *Server) workloads(w http.ResponseWriter, r *http.Request) {
	snapshot, err := s.store.GetLatestSnapshot(r.Context())
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, "storage_unavailable", err.Error())
		return
	}
	if snapshot == nil {
		s.writeJSON(w, http.StatusOK, map[string]interface{}{"items": []interface{}{}, "count": 0})
		return
	}
	namespaceFilter := strings.ToLower(r.URL.Query().Get("namespace"))
	search := strings.ToLower(r.URL.Query().Get("search"))
	items := make([]*model.WorkloadSnapshot, 0)
	for _, namespace := range snapshot.Namespaces {
		for _, workload := range namespace.Workloads {
			if namespaceFilter != "" && strings.ToLower(workload.Namespace) != namespaceFilter {
				continue
			}
			if search != "" && !strings.Contains(strings.ToLower(workload.Name), search) {
				continue
			}
			items = append(items, workload)
		}
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].Namespace == items[j].Namespace {
			return items[i].Name < items[j].Name
		}
		return items[i].Namespace < items[j].Namespace
	})
	s.writeJSON(w, http.StatusOK, map[string]interface{}{"items": items, "count": len(items), "timestamp": snapshot.Timestamp})
}

func (s *Server) workload(w http.ResponseWriter, r *http.Request) {
	snapshot, err := s.store.GetLatestSnapshot(r.Context())
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, "storage_unavailable", err.Error())
		return
	}
	if snapshot != nil {
		if namespace, ok := snapshot.Namespaces[r.PathValue("namespace")]; ok {
			if workload, ok := namespace.Workloads[r.PathValue("name")]; ok {
				s.writeJSON(w, http.StatusOK, map[string]interface{}{"timestamp": snapshot.Timestamp, "workload": workload})
				return
			}
		}
	}
	s.writeError(w, http.StatusNotFound, "workload_not_found", "the requested workload was not found")
}

func (s *Server) recommendations(w http.ResponseWriter, r *http.Request) {
	scope := r.URL.Query().Get("scope")
	if scope == "" {
		scope = "latest"
	}
	if scope != "latest" && scope != "all" {
		s.writeError(w, http.StatusBadRequest, "invalid_scope", "scope must be latest or all")
		return
	}
	recommendations, err := s.store.GetRecommendations(r.Context(), storage.RecommendationQuery{
		Status: r.URL.Query().Get("status"), Limit: parseLimit(r, 100), LatestOnly: scope == "latest",
	})
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, "storage_unavailable", err.Error())
		return
	}
	s.writeJSON(w, http.StatusOK, map[string]interface{}{"items": recommendations, "count": len(recommendations)})
}

func parseLimit(r *http.Request, fallback int) int {
	limit, err := strconv.Atoi(r.URL.Query().Get("limit"))
	if err != nil || limit <= 0 || limit > 500 {
		return fallback
	}
	return limit
}

func (s *Server) withCORS(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := strings.TrimRight(r.Header.Get("Origin"), "/")
		if _, ok := s.allowedOrigins[origin]; ok {
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Vary", "Origin")
		}
		if r.Method == http.MethodOptions {
			w.Header().Set("Access-Control-Allow-Methods", "GET, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) writeJSON(w http.ResponseWriter, status int, value interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(value); err != nil {
		return
	}
}

func (s *Server) writeError(w http.ResponseWriter, status int, code, message string) {
	s.writeJSON(w, status, map[string]interface{}{"error": map[string]string{"code": code, "message": message}})
}
