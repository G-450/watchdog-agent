from fastapi import FastAPI
from pydantic import BaseModel, Field
from typing import Dict, Any, List
from datetime import datetime, timezone

app = FastAPI(title="Watchdog AI Service", version="1.0.0")

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

@app.post("/api/v1/analyze", response_model=List[Recommendation])
async def analyze_snapshot(snapshot: ClusterSnapshot):
    from reasoner import analyze_workloads
    snapshot_dict = snapshot.model_dump()
    recs = analyze_workloads(snapshot_dict)
    
    recommendations = []
    for r in recs:
        recommendations.append(Recommendation(**r))
        
    return recommendations

@app.get("/health")
async def health_check():
    return {"status": "OK"}
