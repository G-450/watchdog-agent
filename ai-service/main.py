from fastapi import FastAPI, HTTPException
from pydantic import BaseModel, Field
from typing import Dict, Any, List, Optional
from datetime import datetime, timezone

from forecaster import generate_multi_horizon_forecast
from reasoner import analyze_workloads

app = FastAPI(
    title="Watchdog AI Reasoning & Forecasting Service",
    description="Time-series forecasting and LangGraph-driven optimization reasoning for Kubernetes workloads.",
    version="1.0.0"
)

class WorkloadForecastRequest(BaseModel):
    name: Optional[str] = Field("workload", description="Workload identifier")
    namespace: Optional[str] = Field("default", description="Workload namespace")
    CPUUsage: float = Field(0.0, description="Current CPU usage in cores")
    MemUsage: float = Field(0.0, description="Current Memory usage in bytes")
    CPURequests: Optional[float] = Field(0.0, description="Allocated CPU requests in cores")
    CPULimits: Optional[float] = Field(0.0, description="Allocated CPU limits in cores")
    MemRequests: Optional[float] = Field(0.0, description="Allocated Memory requests in bytes")
    MemLimits: Optional[float] = Field(0.0, description="Allocated Memory limits in bytes")
    Replicas: Optional[int] = Field(1, description="Current replica count")
    CPUHistory: Optional[List[float]] = Field(default_factory=list, description="Historical CPU usage time-series")
    MemHistory: Optional[List[float]] = Field(default_factory=list, description="Historical Memory usage time-series")

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

class ClusterSnapshot(BaseModel):
    timestamp: datetime
    namespaces: Dict[str, Any]
    nodes: int
    total_cost: float

class Recommendation(BaseModel):
    target: str
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
async def forecast_workload(req: WorkloadForecastRequest):
    """
    Stand-alone multi-horizon forecasting endpoint predicting peak resource
    requirements across 30min, 6h, 24h, and 7d horizons.
    """
    try:
        workload_data = req.model_dump()
        forecast = generate_multi_horizon_forecast(workload_data)
        return forecast
    except Exception as e:
        raise HTTPException(status_code=500, detail=f"Forecasting error: {str(e)}")

@app.post("/api/v1/analyze", response_model=List[Recommendation])
async def analyze_snapshot(snapshot: ClusterSnapshot):
    """
    Comprehensive optimization reasoning endpoint analyzing cluster workloads
    and producing structured, policy-checked recommendations.
    """
    try:
        snapshot_dict = snapshot.model_dump()
        raw_recs = analyze_workloads(snapshot_dict)

        recommendations = []
        for r in raw_recs:
            recommendations.append(Recommendation(**r))

        return recommendations
    except Exception as e:
        raise HTTPException(status_code=500, detail=f"Reasoning error: {str(e)}")
