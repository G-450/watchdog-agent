import json
from typing import Dict, Any, List, TypedDict, Optional
from datetime import datetime, timezone
from langgraph.graph import StateGraph, END
from forecaster import generate_multi_horizon_forecast

# Standard cost estimation constants ($ / unit / month)
CPU_COST_PER_CORE_MONTH = 24.0
MEM_COST_PER_GB_MONTH = 3.2

class AgentState(TypedDict):
    workload_name: str
    namespace: str
    workload: Dict[str, Any]
    forecast: Dict[str, Any]
    utilization_profile: Dict[str, Any]
    candidate_recommendations: List[Dict[str, Any]]
    recommendations: List[Dict[str, Any]]

def forecast_node(state: AgentState) -> Dict[str, Any]:
    """Generates multi-horizon time-series forecast for the workload."""
    forecast = generate_multi_horizon_forecast(state["workload"])
    return {"forecast": forecast}

def utilization_node(state: AgentState) -> Dict[str, Any]:
    """Analyzes current resource allocation against forecasted demand."""
    wl = state["workload"]
    fc = state["forecast"]
    
    cpu_req = float(wl.get("CPURequests") or 0.0)
    cpu_lim = float(wl.get("CPULimits") or 0.0)
    mem_req = float(wl.get("MemRequests") or 0.0)
    mem_lim = float(wl.get("MemLimits") or 0.0)
    replicas = int(wl.get("Replicas") or 1)
    
    expected_peak_cpu = fc.get("expected_peak_cpu", 0.0)
    expected_peak_mem = fc.get("expected_peak_mem", 0.0)
    
    # Calculate utilization metrics
    cpu_util_ratio = (expected_peak_cpu / cpu_req) if cpu_req > 0 else 1.0
    mem_util_ratio = (expected_peak_mem / mem_req) if mem_req > 0 else 1.0
    
    profile = {
        "cpu_requests": cpu_req,
        "cpu_limits": cpu_lim,
        "mem_requests": mem_req,
        "mem_limits": mem_lim,
        "replicas": replicas,
        "expected_peak_cpu": expected_peak_cpu,
        "expected_peak_mem": expected_peak_mem,
        "cpu_util_ratio": cpu_util_ratio,
        "mem_util_ratio": mem_util_ratio,
        "is_idle": (expected_peak_cpu < 0.02 and expected_peak_mem < 50.0),
        "cpu_overprovisioned": (cpu_req > 0 and cpu_util_ratio < 0.50),
        "cpu_throttling_risk": (cpu_req > 0 and cpu_util_ratio >= 0.88),
        "mem_overprovisioned": (mem_req > 0 and mem_util_ratio < 0.50),
        "mem_oom_risk": (mem_req > 0 and mem_util_ratio >= 0.88),
    }
    
    return {"utilization_profile": profile}

def reason_node(state: AgentState) -> Dict[str, Any]:
    """Generates optimization recommendations based on utilization profiles and forecasts."""
    profile = state["utilization_profile"]
    fc = state["forecast"]
    target = f"{state['namespace']}/{state['workload_name']}"
    
    candidates = []
    
    cpu_req = profile["cpu_requests"]
    mem_req = profile["mem_requests"]
    replicas = profile["replicas"]
    expected_peak_cpu = profile["expected_peak_cpu"]
    expected_peak_mem = profile["expected_peak_mem"]
    
    # 1. CPU Rightsizing: Over-provisioned (Scale Down requests with safety buffer)
    if profile["cpu_overprovisioned"]:
        # Safe step down: 30% headroom above expected peak, capped at max 30% step-down per iteration
        min_safe_cpu = round(expected_peak_cpu * 1.30, 3)
        max_allowed_step_down = round(cpu_req * 0.70, 3)
        proposed_cpu = max(min_safe_cpu, max_allowed_step_down)
        
        # Only suggest if there is meaningful difference
        if proposed_cpu < cpu_req * 0.95:
            cpu_delta = cpu_req - proposed_cpu
            savings = round(cpu_delta * replicas * CPU_COST_PER_CORE_MONTH, 2)
            candidates.append({
                "target": target,
                "action": "RIGHTSIZE_CPU_DOWN",
                "current_state": json.dumps({
                    "cpu_requests": round(cpu_req, 3),
                    "replicas": replicas,
                    "mem_requests": round(mem_req, 2)
                }),
                "proposed_state": json.dumps({
                    "cpu_requests": proposed_cpu,
                    "replicas": replicas,
                    "mem_requests": round(mem_req, 2)
                }),
                "expected_savings": max(savings, 0.0),
                "supporting_evidence": (
                    f"Forecasted 24h peak CPU is {expected_peak_cpu:.3f} cores vs requested {cpu_req:.3f} cores "
                    f"({profile['cpu_util_ratio']*100:.1f}% utilization). Proposing step-down to {proposed_cpu:.3f} cores "
                    f"retaining 30% safety headroom."
                ),
                "rule_trace": ["UtilizationProfiled", "OverProvisionedCPURule", "SafetyHeadroomApplied"],
                "safety_factor": 0.92
            })
            
    # 2. CPU Scale-Up: Under-provisioned / Throttling Risk
    elif profile["cpu_throttling_risk"]:
        proposed_cpu = round(max(expected_peak_cpu * 1.25, cpu_req * 1.20), 3)
        candidates.append({
            "target": target,
            "action": "SCALE_UP_CPU_SAFETY",
            "current_state": json.dumps({
                "cpu_requests": round(cpu_req, 3),
                "replicas": replicas,
                "mem_requests": round(mem_req, 2)
            }),
            "proposed_state": json.dumps({
                "cpu_requests": proposed_cpu,
                "replicas": replicas,
                "mem_requests": round(mem_req, 2)
            }),
            "expected_savings": 0.0,
            "supporting_evidence": (
                f"Peak CPU forecast ({expected_peak_cpu:.3f} cores) reaches {profile['cpu_util_ratio']*100:.1f}% "
                f"of requested capacity ({cpu_req:.3f} cores). Scaling up to prevent throttling and SLA breach."
            ),
            "rule_trace": ["UtilizationProfiled", "UnderProvisionedCPURule", "ThrottlingPreventionSafeguard"],
            "safety_factor": 0.88
        })

    # 3. Replica Rightsizing: High replica count with low aggregated load
    if replicas > 2 and profile["cpu_util_ratio"] < 0.25 and fc.get("trend") != "increasing":
        proposed_replicas = max(2, replicas - 1)
        if proposed_replicas < replicas:
            rep_savings = round((cpu_req * CPU_COST_PER_CORE_MONTH + (mem_req / (1024**3)) * MEM_COST_PER_GB_MONTH) * (replicas - proposed_replicas), 2)
            candidates.append({
                "target": target,
                "action": "RIGHTSIZE_REPLICAS",
                "current_state": json.dumps({
                    "replicas": replicas,
                    "cpu_requests": round(cpu_req, 3),
                }),
                "proposed_state": json.dumps({
                    "replicas": proposed_replicas,
                    "cpu_requests": round(cpu_req, 3),
                }),
                "expected_savings": max(rep_savings, 5.0),
                "supporting_evidence": (
                    f"Workload replicas ({replicas}) exhibit low aggregate load ({profile['cpu_util_ratio']*100:.1f}%) "
                    f"with stable demand trend. Safely consolidating to {proposed_replicas} replicas."
                ),
                "rule_trace": ["ReplicaLoadProfiled", "LowReplicaUtilizationRule", "MinReplicasSafeguard"],
                "safety_factor": 0.85
            })

    return {"candidate_recommendations": candidates}

def confidence_node(state: AgentState) -> Dict[str, Any]:
    """Calculates multi-factor confidence score for each recommendation candidate."""
    fc = state["forecast"]
    forecast_certainty = float(fc.get("confidence") or 0.80)
    telemetry_quality = float(fc.get("telemetry_quality") or 0.80)
    
    scored_recs = []
    for cand in state.get("candidate_recommendations", []):
        safety_factor = cand.get("safety_factor", 0.85)
        
        # Composite score formula:
        # 40% Forecast Certainty + 30% Telemetry Quality + 30% Change Safety Margin
        composite_score = (
            0.40 * forecast_certainty +
            0.30 * telemetry_quality +
            0.30 * safety_factor
        )
        composite_score = round(min(max(composite_score, 0.1), 0.99), 2)
        
        cand["confidence_score"] = composite_score
        cand["rule_trace"].append(f"ConfidenceScored:{composite_score}")
        scored_recs.append(cand)
        
    return {"candidate_recommendations": scored_recs}

def policy_node(state: AgentState) -> Dict[str, Any]:
    """Evaluates organizational guardrails and establishes recommendation status."""
    EXCLUDED_NAMESPACES = {"kube-system", "monitoring", "watchdog"}
    
    final_recs = []
    now_iso = datetime.now(timezone.utc).isoformat()
    
    for cand in state.get("candidate_recommendations", []):
        rule_trace = list(cand["rule_trace"])
        status = "Pending"
        rejection_reason = ""
        
        # 1. Namespace exclusion check
        if state["namespace"] in EXCLUDED_NAMESPACES:
            status = "Rejected"
            rejection_reason = f"Namespace '{state['namespace']}' is protected from automated rightsizing."
            rule_trace.append("PolicyRejected:ExcludedNamespace")
        else:
            rule_trace.append("PolicyApproved:NamespaceAllowed")
            
        rec = {
            "target": cand["target"],
            "current_state": cand["current_state"],
            "proposed_state": cand["proposed_state"],
            "expected_savings": cand["expected_savings"],
            "confidence_score": cand["confidence_score"],
            "supporting_evidence": cand["supporting_evidence"],
            "rule_trace": rule_trace,
            "status": status,
            "rejection_reason": rejection_reason,
            "timestamp": now_iso
        }
        final_recs.append(rec)
        
    return {"recommendations": final_recs}

def build_graph():
    workflow = StateGraph(AgentState)
    
    workflow.add_node("forecast", forecast_node)
    workflow.add_node("utilization", utilization_node)
    workflow.add_node("reason", reason_node)
    workflow.add_node("confidence", confidence_node)
    workflow.add_node("policy", policy_node)
    
    workflow.set_entry_point("forecast")
    workflow.add_edge("forecast", "utilization")
    workflow.add_edge("utilization", "reason")
    workflow.add_edge("reason", "confidence")
    workflow.add_edge("confidence", "policy")
    workflow.add_edge("policy", END)
    
    return workflow.compile()

graph = build_graph()

def analyze_workloads(snapshot: Dict[str, Any]) -> List[Dict[str, Any]]:
    """Analyzes all workloads in a cluster snapshot and returns optimization recommendations."""
    all_recs = []
    namespaces = snapshot.get("namespaces", {})
    
    for ns_name, ns_data in namespaces.items():
        workloads = ns_data.get("Workloads", {})
        for wl_name, wl_data in workloads.items():
            state = {
                "workload_name": wl_name,
                "namespace": ns_name,
                "workload": wl_data,
                "forecast": {},
                "utilization_profile": {},
                "candidate_recommendations": [],
                "recommendations": []
            }
            
            result = graph.invoke(state)
            all_recs.extend(result.get("recommendations", []))
            
    return all_recs
