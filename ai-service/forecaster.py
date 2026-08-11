from typing import Dict, Any

def generate_forecast(workload: Dict[str, Any]) -> Dict[str, Any]:
    """
    Generates a simple heuristic forecast based on current usage.
    In the future, this can be replaced with an LSTM or PatchTST model.
    """
    cpu_usage = workload.get("CPUUsage", 0.0)
    mem_usage = workload.get("MemUsage", 0.0)

    # Heuristic: Add a 20% buffer for peaks
    forecasted_cpu = cpu_usage * 1.2
    forecasted_mem = mem_usage * 1.2

    return {
        "expected_peak_cpu": forecasted_cpu,
        "expected_peak_mem": forecasted_mem,
        "confidence": 0.8  # Simple heuristic confidence
    }
