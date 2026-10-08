import pytest
import sys
import os

sys.path.insert(0, os.path.abspath(os.path.join(os.path.dirname(__file__), "..")))

from reasoner import analyze_workloads
from forecaster import generate_forecast

def test_generate_forecast_compatibility():
    workload = {"cpu_usage": 1.0, "mem_usage": 1024.0}
    forecast = generate_forecast(workload)
    assert forecast["expected_peak_cpu"] > 1.0
    assert forecast["expected_peak_mem"] > 1024.0
    assert forecast["confidence"] > 0.5

def test_reasoner_overprovisioned_rightsizing():
    snapshot = {
        "namespaces": {
            "default": {
                "workloads": {
                    "overprovisioned-app": {
                        "cpu_usage": 0.1,
                        "cpu_requests": 1.0,
                        "mem_usage": 200.0,
                        "mem_requests": 1024.0,
                        "replicas": 3
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
                "workloads": {
                    "high-traffic-service": {
                        "cpu_usage": 1.9,
                        "cpu_requests": 1.0,
                        "mem_usage": 900.0,
                        "mem_requests": 1024.0,
                        "replicas": 2
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
                "workloads": {
                    "over-replicated-api": {
                        "cpu_usage": 0.05,
                        "cpu_requests": 1.0,
                        "replicas": 5
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
                "workloads": {
                    "coredns": {
                        "cpu_usage": 0.01,
                        "cpu_requests": 1.0,
                        "replicas": 2
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
