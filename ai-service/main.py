from fastapi import FastAPI, HTTPException
from pydantic import BaseModel, Field
from typing import Dict, List, Optional
from datetime import datetime

from forecaster import generate_multi_horizon_forecast
from reasoner import analyze_workloads

app = FastAPI(
    title="Watchdog AI Reasoning & Forecasting Service",
    description="Time-series forecasting and LangGraph-driven optimization reasoning for Kubernetes workloads.",
    version="1.1.0"
)

# Field names and units mirror the Go agent's model.WorkloadSnapshot JSON. The shared
# fixture in ../testdata/analyze_request.json pins this contract on both sides.

class WorkloadSnapshot(BaseModel):
    name: str = Field("workload", description="Workload name")
    namespace: str = Field("default", description="Workload namespace")
    type: str = Field("Deployment", description="Workload kind")
    replicas: int = Field(1, description="Current replica count")
    cpu_requests: float = Field(0.0, description="CPU request per replica, in cores")
    cpu_limits: float = Field(0.0, description="CPU limit per replica, in cores")
    mem_requests: float = Field(0.0, description="Memory request per replica, in bytes")
    mem_limits: float = Field(0.0, description="Memory limit per replica, in bytes")
    cpu_usage: float = Field(0.0, description="CPU usage summed across replicas, in cores")
    mem_usage: float = Field(0.0, description="Memory working set summed across replicas, in bytes")
    net_rx_usage: float = Field(0.0, description="Network receive rate, in bytes per second")
    net_tx_usage: float = Field(0.0, description="Network transmit rate, in bytes per second")
    total_cost: float = Field(0.0, description="Monthly run-rate cost, in USD")
    is_excluded: bool = Field(False, description="Workload opted out of optimization")
    exclude_reason: str = ""
    service_dependencies: Optional[List[str]] = None
    cpu_history: List[float] = Field(default_factory=list, description="Recent CPU usage (same units as cpu_usage), oldest first")
    mem_history: List[float] = Field(default_factory=list, description="Recent memory usage (same units as mem_usage), oldest first")

class NamespaceSnapshot(BaseModel):
    name: str = ""
    workloads: Dict[str, WorkloadSnapshot] = Field(default_factory=dict)
    namespace_cost: float = 0.0

class ClusterSnapshot(BaseModel):
    timestamp: datetime
    cluster_id: str = ""
    namespaces: Dict[str, NamespaceSnapshot] = Field(default_factory=dict)
    nodes: int = 0
    total_cost: float = 0.0

class HorizonPrediction(BaseModel):
    peak_cpu: float
    peak_mem: float
    confidence: float

class ForecastResponse(BaseModel):
    expected_peak_cpu: float
    expected_peak_mem: float
    confidence: float
    trend: str
    telemetry_quality: float
    horizons: Dict[str, HorizonPrediction]

class Recommendation(BaseModel):
    target: str
    action: str
    current_state: str
    proposed_state: str
    expected_savings: float
    confidence_score: float
    supporting_evidence: str
    rule_trace: List[str]
    status: str
    rejection_reason: str = ""
    timestamp: datetime

@app.get("/health")
async def health_check():
    return {"status": "OK"}

@app.get("/ready")
async def readiness_check():
    return {"status": "Ready", "models_loaded": True, "engine": "LangGraph"}

@app.post("/api/v1/forecast", response_model=ForecastResponse)
async def forecast_workload(req: WorkloadSnapshot):
    """
    Stand-alone multi-horizon forecasting endpoint predicting peak resource
    requirements across 30min, 6h, 24h, and 7d horizons.
    """
    try:
        return generate_multi_horizon_forecast(req.model_dump())
    except Exception as e:
        raise HTTPException(status_code=500, detail=f"Forecasting error: {str(e)}")

@app.post("/api/v1/analyze", response_model=List[Recommendation])
async def analyze_snapshot(snapshot: ClusterSnapshot):
    """
    Comprehensive optimization reasoning endpoint analyzing cluster workloads
    and producing structured, policy-checked recommendations.
    """
    try:
        raw_recs = analyze_workloads(snapshot.model_dump())
        return [Recommendation(**r) for r in raw_recs]
    except Exception as e:
        raise HTTPException(status_code=500, detail=f"Reasoning error: {str(e)}")
