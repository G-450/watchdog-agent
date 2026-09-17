import pytest
import sys
import os

sys.path.insert(0, os.path.abspath(os.path.join(os.path.dirname(__file__), "..")))

from forecaster import generate_multi_horizon_forecast, generate_forecast

def test_generate_multi_horizon_forecast_basic():
    workload = {
        "CPUUsage": 1.0,
        "MemUsage": 1024.0,
        "CPURequests": 2.0,
        "MemRequests": 2048.0,
        "Replicas": 2
    }
    
    forecast = generate_multi_horizon_forecast(workload)
    
    assert "expected_peak_cpu" in forecast
    assert "expected_peak_mem" in forecast
    assert "horizons" in forecast
    assert "30min" in forecast["horizons"]
    assert "6h" in forecast["horizons"]
    assert "24h" in forecast["horizons"]
    assert "7d" in forecast["horizons"]
    
    # 24h is the primary headline peak
    assert forecast["expected_peak_cpu"] == forecast["horizons"]["24h"]["peak_cpu"]
    assert forecast["expected_peak_mem"] == forecast["horizons"]["24h"]["peak_mem"]
    
    # Check buffer multipliers scale with horizon
    h = forecast["horizons"]
    assert h["30min"]["peak_cpu"] < h["6h"]["peak_cpu"] < h["24h"]["peak_cpu"] < h["7d"]["peak_cpu"]
    
    # Confidence should decay as horizon extends into future
    assert h["30min"]["confidence"] >= h["6h"]["confidence"] >= h["24h"]["confidence"] >= h["7d"]["confidence"]

def test_forecaster_with_historical_series():
    # Simulating upward trending usage
    cpu_history = [0.2, 0.25, 0.3, 0.35, 0.4, 0.45, 0.5]
    mem_history = [200.0, 220.0, 240.0, 260.0, 280.0, 300.0]
    
    workload = {
        "CPUUsage": 0.5,
        "MemUsage": 300.0,
        "CPUHistory": cpu_history,
        "MemHistory": mem_history
    }
    
    forecast = generate_multi_horizon_forecast(workload)
    
    assert forecast["trend"] == "increasing"
    assert forecast["telemetry_quality"] >= 0.8
    assert forecast["expected_peak_cpu"] > 0.5

def test_forecaster_edge_cases():
    # Zero usage workload
    zero_workload = {"CPUUsage": 0.0, "MemUsage": 0.0}
    zero_forecast = generate_multi_horizon_forecast(zero_workload)
    assert zero_forecast["expected_peak_cpu"] == 0.0
    assert zero_forecast["expected_peak_mem"] == 0.0
    assert zero_forecast["confidence"] > 0.0

def test_backwards_compatible_generate_forecast():
    workload = {"CPUUsage": 0.5, "MemUsage": 512.0}
    forecast = generate_forecast(workload)
    assert forecast["expected_peak_cpu"] > 0.5
    assert forecast["expected_peak_mem"] > 512.0
    assert "confidence" in forecast
