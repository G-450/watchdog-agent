import pytest
import sys
import os
from fastapi.testclient import TestClient
from datetime import datetime, timezone

sys.path.insert(0, os.path.abspath(os.path.join(os.path.dirname(__file__), "..")))

from main import app

client = TestClient(app)

def test_health_check():
    response = client.get("/health")
    assert response.status_code == 200
    assert response.json() == {"status": "OK"}

def test_readiness_check():
    response = client.get("/ready")
    assert response.status_code == 200
    data = response.json()
    assert data["status"] == "Ready"
    assert data["models_loaded"] is True

def test_api_forecast_endpoint():
    payload = {
        "name": "cartservice",
        "namespace": "online-boutique",
        "cpu_usage": 0.45,
        "mem_usage": 512000000.0,
        "cpu_requests": 1.0,
        "mem_requests": 1024000000.0,
        "replicas": 3,
        "cpu_history": [0.35, 0.38, 0.40, 0.42, 0.45]
    }
    
    response = client.post("/api/v1/forecast", json=payload)
    assert response.status_code == 200
    data = response.json()
    
    assert "expected_peak_cpu" in data
    assert "expected_peak_mem" in data
    assert "confidence" in data
    assert "horizons" in data
    assert len(data["horizons"]) == 4
    assert "30min" in data["horizons"]
    assert "6h" in data["horizons"]
    assert "24h" in data["horizons"]
    assert "7d" in data["horizons"]

def test_api_analyze_endpoint():
    payload = {
        "timestamp": datetime.now(timezone.utc).isoformat(),
        "nodes": 3,
        "total_cost": 150.0,
        "namespaces": {
            "default": {
                "workloads": {
                    "frontend": {
                        "cpu_usage": 0.08,
                        "cpu_requests": 1.0,
                        "mem_usage": 100000000.0,
                        "mem_requests": 1000000000.0,
                        "replicas": 4
                    }
                }
            }
        }
    }
    
    response = client.post("/api/v1/analyze", json=payload)
    assert response.status_code == 200
    recs = response.json()
    assert isinstance(recs, list)
    assert len(recs) >= 1
    
    rec = recs[0]
    assert rec["target"] == "default/frontend"
    assert "confidence_score" in rec
    assert rec["confidence_score"] > 0
    assert "rule_trace" in rec
    assert len(rec["rule_trace"]) > 0
    assert "supporting_evidence" in rec
