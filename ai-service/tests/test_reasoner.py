import pytest
import sys
import os
sys.path.insert(0, os.path.abspath(os.path.join(os.path.dirname(__file__), '..')))

from reasoner import analyze_workloads
from forecaster import generate_forecast

def test_generate_forecast():
    workload = {"CPUUsage": 1.0, "MemUsage": 1024.0}
    forecast = generate_forecast(workload)
    assert forecast["expected_peak_cpu"] == 1.2
    assert forecast["expected_peak_mem"] == 1228.8
    assert forecast["confidence"] == 0.8

def test_analyze_workloads():
    snapshot = {
        "namespaces": {
            "default": {
                "Workloads": {
                    "my-app": {
                        "CPUUsage": 0.1,
                        "CPURequests": 1.0,
                        "Replicas": 3
                    }
                }
            }
        }
    }
    
    recs = analyze_workloads(snapshot)
    assert len(recs) == 1
    rec = recs[0]
    assert rec["target"] == "default/my-app"
    assert rec["expected_savings"] > 0
    assert rec["status"] == "Pending"
