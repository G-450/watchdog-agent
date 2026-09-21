import math
from typing import Dict, Any, List, Optional

HORIZONS = {
    "30min": {"hours": 0.5, "buffer_multiplier": 1.15, "base_confidence": 0.92},
    "6h":    {"hours": 6.0, "buffer_multiplier": 1.25, "base_confidence": 0.85},
    "24h":   {"hours": 24.0, "buffer_multiplier": 1.35, "base_confidence": 0.78},
    "7d":    {"hours": 168.0, "buffer_multiplier": 1.50, "base_confidence": 0.65},
}

def _calculate_series_stats(series: List[float]) -> Dict[str, float]:
    """Computes basic statistical metrics for a numeric sequence."""
    if not series:
        return {"mean": 0.0, "std": 0.0, "min": 0.0, "max": 0.0, "trend": 0.0}
    
    n = len(series)
    mean_val = sum(series) / n
    variance = sum((x - mean_val) ** 2 for x in series) / n if n > 1 else 0.0
    std_val = math.sqrt(variance)
    
    # Simple linear slope estimation for trend
    if n >= 2:
        x_mean = (n - 1) / 2.0
        numerator = sum((i - x_mean) * (val - mean_val) for i, val in enumerate(series))
        denominator = sum((i - x_mean) ** 2 for i in range(n))
        slope = numerator / denominator if denominator != 0 else 0.0
    else:
        slope = 0.0
        
    return {
        "mean": mean_val,
        "std": std_val,
        "min": min(series),
        "max": max(series),
        "trend": slope
    }

def generate_multi_horizon_forecast(workload: Dict[str, Any]) -> Dict[str, Any]:
    """
    Generates multi-horizon time-series predictions (30min, 6h, 24h, 7d)
    for CPU and Memory usage with confidence scoring and volatility bounds.
    """
    cpu_usage = float(workload.get("cpu_usage") or 0.0)
    mem_usage = float(workload.get("mem_usage") or 0.0)
    
    cpu_history = workload.get("cpu_history") or []
    mem_history = workload.get("mem_history") or []
    
    # Assess telemetry quality based on available signals
    quality_factors = []
    if cpu_usage > 0:
        quality_factors.append(0.3)
    if mem_usage > 0:
        quality_factors.append(0.3)
    if cpu_history and len(cpu_history) >= 5:
        quality_factors.append(0.2)
    if mem_history and len(mem_history) >= 5:
        quality_factors.append(0.2)
    
    telemetry_quality = sum(quality_factors) if quality_factors else 0.4
    telemetry_quality = min(max(telemetry_quality, 0.2), 1.0)
    
    cpu_stats = _calculate_series_stats(cpu_history) if cpu_history else None
    mem_stats = _calculate_series_stats(mem_history) if mem_history else None
    
    # Determine overall trend classification
    if cpu_stats and abs(cpu_stats["trend"]) > 0.01:
        trend = "increasing" if cpu_stats["trend"] > 0 else "decreasing"
    else:
        trend = "stable"
        
    horizons_output = {}
    
    for h_name, h_params in HORIZONS.items():
        h_hours = h_params["hours"]
        buf = h_params["buffer_multiplier"]
        base_conf = h_params["base_confidence"]
        
        # CPU forecast for this horizon
        if cpu_stats and len(cpu_history) >= 3:
            # Extrapolate mean with trend, bounded to non-negative
            proj_mean = max(0.0, cpu_stats["mean"] + (cpu_stats["trend"] * min(h_hours, 24.0)))
            # Headroom based on volatility (mean + 1.8 * std)
            projected_peak_cpu = max(proj_mean + (1.8 * cpu_stats["std"]), cpu_stats["max"] * (1.05 + 0.05 * (h_hours / 24.0)))
            # Adjust confidence for variance
            cv = (cpu_stats["std"] / cpu_stats["mean"]) if cpu_stats["mean"] > 0 else 0.0
            conf_decay = max(0.0, 1.0 - (0.15 * cv))
            h_confidence = base_conf * conf_decay
        else:
            projected_peak_cpu = cpu_usage * buf
            h_confidence = base_conf * 0.9  # slight penalty for no historical series
            
        # Memory forecast for this horizon
        if mem_stats and len(mem_history) >= 3:
            proj_mem_mean = max(0.0, mem_stats["mean"] + (mem_stats["trend"] * min(h_hours, 24.0)))
            projected_peak_mem = max(proj_mem_mean + (1.8 * mem_stats["std"]), mem_stats["max"] * (1.05 + 0.05 * (h_hours / 24.0)))
        else:
            projected_peak_mem = mem_usage * buf
            
        horizons_output[h_name] = {
            "peak_cpu": round(projected_peak_cpu, 4),
            "peak_mem": round(projected_peak_mem, 2),
            "confidence": round(min(max(h_confidence, 0.1), 0.99), 2)
        }
        
    # Primary headline forecast uses 24h horizon
    primary_24h = horizons_output["24h"]
    
    return {
        "expected_peak_cpu": primary_24h["peak_cpu"],
        "expected_peak_mem": primary_24h["peak_mem"],
        "confidence": primary_24h["confidence"],
        "trend": trend,
        "telemetry_quality": round(telemetry_quality, 2),
        "horizons": horizons_output
    }

def generate_forecast(workload: Dict[str, Any]) -> Dict[str, Any]:
    """
    Backwards-compatible interface returning headline expected peaks and full horizon details.
    """
    return generate_multi_horizon_forecast(workload)
