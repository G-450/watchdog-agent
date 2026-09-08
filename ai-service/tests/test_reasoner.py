import pytest
import sys
import os

sys.path.insert(0, os.path.abspath(os.path.join(os.path.dirname(__file__), "..")))

from reasoner import analyze_workloads
from forecaster import generate_forecast

def test_generate_forecast_compatibility():
    workload = {"CPUUsage": 1.0, "MemUsage": 1024.0}
    forecast = generate_forecast(workload)
    assert forecast["expected_peak_cpu"] > 1.0
    assert forecast["expected_peak_mem"] > 1024.0
    assert forecast["confidence"] > 0.5

def test_reasoner_overprovisioned_rightsizing():
    snapshot = {
        "namespaces": {
            "default": {
                "Workloads": {
                    "overprovisioned-app": {
                        "CPUUsage": 0.1,
                        "CPURequests": 1.0,
                        "MemUsage": 200.0,
                        "MemRequests": 1024.0,
                        "Replicas": 3
                    }
                }
            }
        }
    }
    
    recs = analyze_workloads(snapshot)
    assert len(recs) >= 1
    
    cpu_rec = next((r for r in recs if "OverProvisionedCPURule" in r["rule_trace"]), None)
    assert cpu_rec is not None
    assert cpu_rec["target"] == "default/overprovisioned-app"
    assert cpu_rec["expected_savings"] > 0
    assert cpu_rec["status"] == "Pending"
    assert 0.0 < cpu_rec["confidence_score"] <= 1.0
    assert any("ConfidenceScored" in rule for rule in cpu_rec["rule_trace"])
    assert "PolicyApproved:NamespaceAllowed" in cpu_rec["rule_trace"]

def test_reasoner_underprovisioned_scale_up():
    snapshot = {
        "namespaces": {
            "production": {
                "Workloads": {
                    "high-traffic-service": {
                        "CPUUsage": 0.95,
                        "CPURequests": 1.0,
                        "MemUsage": 900.0,
                        "MemRequests": 1024.0,
                        "Replicas": 2
                    }
                }
            }
        }
    }
    
    recs = analyze_workloads(snapshot)
    assert len(recs) >= 1
    
    safety_rec = next((r for r in recs if "UnderProvisionedCPURule" in r["rule_trace"]), None)
    assert safety_rec is not None
    assert safety_rec["target"] == "production/high-traffic-service"
    assert "ThrottlingPreventionSafeguard" in safety_rec["rule_trace"]
    assert safety_rec["status"] == "Pending"

def test_reasoner_replica_rightsizing():
    snapshot = {
        "namespaces": {
            "default": {
                "Workloads": {
                    "over-replicated-api": {
                        "CPUUsage": 0.05,
                        "CPURequests": 1.0,
                        "Replicas": 5
                    }
                }
            }
        }
    }
    
    recs = analyze_workloads(snapshot)
    rep_rec = next((r for r in recs if "LowReplicaUtilizationRule" in r["rule_trace"]), None)
    assert rep_rec is not None
    assert "MinReplicasSafeguard" in rep_rec["rule_trace"]

def test_reasoner_namespace_policy_rejection():
    snapshot = {
        "namespaces": {
            "kube-system": {
                "Workloads": {
                    "coredns": {
                        "CPUUsage": 0.01,
                        "CPURequests": 1.0,
                        "Replicas": 2
                    }
                }
            }
        }
    }
    
    recs = analyze_workloads(snapshot)
    for rec in recs:
        if rec["target"] == "kube-system/coredns":
            assert rec["status"] == "Rejected"
            assert "PolicyRejected:ExcludedNamespace" in rec["rule_trace"]
            assert "protected" in rec["rejection_reason"]
