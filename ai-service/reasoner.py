import json
from typing import Dict, Any, List, TypedDict
from datetime import datetime, timezone
from langgraph.graph import StateGraph, END
from forecaster import generate_forecast

class AgentState(TypedDict):
    workload_name: str
    namespace: str
    workload: Dict[str, Any]
    forecast: Dict[str, Any]
    recommendations: List[Dict[str, Any]]

def forecast_node(state: AgentState) -> AgentState:
    forecast = generate_forecast(state["workload"])
    return {"forecast": forecast}

def reason_node(state: AgentState) -> AgentState:
    workload = state["workload"]
    forecast = state["forecast"]
    
    current_cpu_req = workload.get("CPURequests", 0.0)
    current_replicas = workload.get("Replicas", 1)
    
    expected_peak_cpu = forecast["expected_peak_cpu"]
    
    recommendations = []
    
    # Over-provisioning check
    if current_cpu_req > 0 and expected_peak_cpu < (current_cpu_req * 0.5):
        proposed_cpu = current_cpu_req * 0.8 # step down 20%
        rec = {
            "target": f"{state['namespace']}/{state['workload_name']}",
            "current_state": json.dumps({"cpu_requests": current_cpu_req, "replicas": current_replicas}),
            "proposed_state": json.dumps({"cpu_requests": proposed_cpu, "replicas": current_replicas}),
            "expected_savings": float((current_cpu_req - proposed_cpu) * 10.0), # dummy calculation
            "confidence_score": float(forecast["confidence"]),
            "supporting_evidence": f"Expected peak CPU {expected_peak_cpu:.2f} is well below requested {current_cpu_req:.2f}",
            "rule_trace": ["HeuristicOverProvisionedRule"],
            "status": "Pending",
            "timestamp": datetime.now(timezone.utc).isoformat()
        }
        recommendations.append(rec)
        
    return {"recommendations": recommendations}

def build_graph():
    workflow = StateGraph(AgentState)
    
    workflow.add_node("forecast", forecast_node)
    workflow.add_node("reason", reason_node)
    
    workflow.set_entry_point("forecast")
    workflow.add_edge("forecast", "reason")
    workflow.add_edge("reason", END)
    
    return workflow.compile()

graph = build_graph()

def analyze_workloads(snapshot: Dict[str, Any]) -> List[Dict[str, Any]]:
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
                "recommendations": []
            }
            
            result = graph.invoke(state)
            all_recs.extend(result.get("recommendations", []))
            
    return all_recs
