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
    # Dummy mock implementation
    return [
        Recommendation(
            target="default/mock-app",
            current_state='{"replicas": 3, "cpu_requests": 1.0}',
            proposed_state='{"replicas": 2, "cpu_requests": 0.8}',
            expected_savings=15.0,
            confidence_score=0.9,
            supporting_evidence="Mock recommendation for testing",
            rule_trace=["MockRule"],
            status="Pending",
            timestamp=datetime.now(timezone.utc),
        )
    ]

@app.get("/health")
async def health_check():
    return {"status": "OK"}
