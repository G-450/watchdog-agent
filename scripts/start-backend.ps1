[CmdletBinding()]
param(
    [string]$SessionName = "watchdog",
    [string]$PrometheusService = "prometheus-stack-kube-prom-prometheus",
    [string]$PrometheusNamespace = "monitoring",
    [string]$OpenCostService = "opencost",
    [string]$OpenCostNamespace = "opencost",
    [switch]$Recreate,
    [switch]$NoAttach,
    [switch]$SkipDependencyInstall,
    [switch]$Stop
)

$ErrorActionPreference = "Stop"
Set-StrictMode -Version Latest
if (Test-Path variable:PSNativeCommandUseErrorActionPreference) {
    $PSNativeCommandUseErrorActionPreference = $false
}

$agentRoot = Split-Path -Parent $PSScriptRoot
$workspaceRoot = Split-Path -Parent $agentRoot
$aiServiceRoot = Join-Path $agentRoot "ai-service"
$requirementsPath = Join-Path $aiServiceRoot "requirements.txt"
$venvRoot = Join-Path $agentRoot ".venv"
$venvPython = Join-Path $venvRoot "Scripts\python.exe"

function Get-RequiredCommand {
    param([Parameter(Mandatory)][string[]]$Names)

    foreach ($name in $Names) {
        $command = Get-Command $name -ErrorAction SilentlyContinue
        if ($command) {
            return $command.Source
        }
    }

    throw "Required command not found: $($Names -join ' or '). Ensure it is installed and available in PATH."
}

function Invoke-Mux {
    param(
        [Parameter(Mandatory)][string]$Executable,
        [Parameter(Mandatory)][string[]]$Arguments,
        [switch]$AllowFailure
    )

    & $Executable @Arguments
    $exitCode = $LASTEXITCODE
    if (-not $AllowFailure -and $exitCode -ne 0) {
        throw "psmux command failed with exit code ${exitCode}: $($Arguments -join ' ')"
    }
    return $exitCode
}

function Test-MuxSession {
    param(
        [Parameter(Mandatory)][string]$Executable,
        [Parameter(Mandatory)][string]$Name
    )

    & $Executable has-session -t $Name 2>$null
    return $LASTEXITCODE -eq 0
}

function Assert-PortAvailable {
    param([Parameter(Mandatory)][int]$Port)

    $listener = Get-NetTCPConnection -State Listen -LocalPort $Port -ErrorAction SilentlyContinue
    if ($listener) {
        throw "Port $Port is already in use. Stop the existing process or attach to the existing psmux session."
    }
}

function Assert-KubernetesService {
    param(
        [Parameter(Mandatory)][string]$Kubectl,
        [Parameter(Mandatory)][string]$Namespace,
        [Parameter(Mandatory)][string]$Service
    )

    & $Kubectl get service $Service --namespace $Namespace --output name *> $null
    if ($LASTEXITCODE -ne 0) {
        throw "Kubernetes service '$Service' was not found in namespace '$Namespace'. Run: kubectl get svc -n $Namespace"
    }
}

function New-MuxWindow {
    param(
        [Parameter(Mandatory)][string]$Executable,
        [Parameter(Mandatory)][string]$Session,
        [Parameter(Mandatory)][string]$Name,
        [Parameter(Mandatory)][string]$WorkingDirectory,
        [Parameter(Mandatory)][string]$Command
    )

    Invoke-Mux -Executable $Executable -Arguments @(
        "new-window", "-d", "-t", $Session, "-n", $Name,
        "-c", $WorkingDirectory, "--",
        $pwsh, "-NoLogo", "-NoExit", "-Command", $Command
    ) | Out-Null
}

$mux = Get-RequiredCommand -Names @("psmux", "tmux", "pmux")

if ($Stop) {
    if (Test-MuxSession -Executable $mux -Name $SessionName) {
        Invoke-Mux -Executable $mux -Arguments @("kill-session", "-t", $SessionName) | Out-Null
        Write-Host "Stopped psmux session '$SessionName'." -ForegroundColor Green
    } else {
        Write-Host "No psmux session named '$SessionName' is running." -ForegroundColor Yellow
    }
    return
}

if (Test-MuxSession -Executable $mux -Name $SessionName) {
    if ($Recreate) {
        Invoke-Mux -Executable $mux -Arguments @("kill-session", "-t", $SessionName) | Out-Null
        Start-Sleep -Milliseconds 500
    } else {
        Write-Host "Session '$SessionName' is already running." -ForegroundColor Yellow
        if (-not $NoAttach) {
            Invoke-Mux -Executable $mux -Arguments @("attach-session", "-t", $SessionName) | Out-Null
        }
        return
    }
}

$kubectl = Get-RequiredCommand -Names @("kubectl")
$go = Get-RequiredCommand -Names @("go")
$pwsh = Get-RequiredCommand -Names @("pwsh")

Write-Host "Checking Kubernetes access..." -ForegroundColor Cyan
& $kubectl cluster-info *> $null
if ($LASTEXITCODE -ne 0) {
    throw "The current Kubernetes context is unavailable. Run 'kubectl config current-context' and 'kubectl cluster-info'."
}

Assert-KubernetesService -Kubectl $kubectl -Namespace $PrometheusNamespace -Service $PrometheusService
Assert-KubernetesService -Kubectl $kubectl -Namespace $OpenCostNamespace -Service $OpenCostService

foreach ($port in @(9090, 9003, 8000, 8081)) {
    Assert-PortAvailable -Port $port
}

if (-not (Test-Path -LiteralPath $venvPython)) {
    throw "Existing Python virtual environment not found at '$venvRoot'. Expected interpreter: '$venvPython'."
}

& $venvPython -c "import fastapi, uvicorn, langgraph, langchain, pandas, pydantic" 2>$null
$dependenciesReady = $LASTEXITCODE -eq 0
if (-not $dependenciesReady) {
    if ($SkipDependencyInstall) {
        throw "AI service dependencies are missing. Rerun without -SkipDependencyInstall."
    }

    Write-Host "Installing AI service dependencies..." -ForegroundColor Cyan
    & $venvPython -m pip install --upgrade pip
    if ($LASTEXITCODE -ne 0) {
        throw "Failed to upgrade pip."
    }
    & $venvPython -m pip install -r $requirementsPath
    if ($LASTEXITCODE -ne 0) {
        throw "Failed to install AI service dependencies."
    }
}

$escapedKubectl = $kubectl.Replace("'", "''")
$escapedGo = $go.Replace("'", "''")
$prometheusCommand = "Write-Host 'Prometheus -> http://localhost:9090' -ForegroundColor Green; & '$escapedKubectl' port-forward svc/$PrometheusService -n $PrometheusNamespace 9090:9090"
$openCostCommand = "Write-Host 'OpenCost -> http://localhost:9003' -ForegroundColor Green; & '$escapedKubectl' port-forward svc/$OpenCostService -n $OpenCostNamespace 9003:9003"
$escapedPython = $venvPython.Replace("'", "''")
$aiCommand = "Write-Host 'AI service -> http://localhost:8000' -ForegroundColor Green; & '$escapedPython' -m uvicorn main:app --host 0.0.0.0 --port 8000 --reload"
$agentCommand = @"
`$env:WATCHDOG_PROMETHEUS_URL = "http://localhost:9090"
`$env:WATCHDOG_OPENCOST_URL = "http://localhost:9003"
`$env:WATCHDOG_AI_SERVICE_URL = "http://localhost:8000"
`$env:WATCHDOG_STORAGE_PATH = "./data.db"
`$env:WATCHDOG_API_ALLOWED_ORIGINS = "http://localhost:3000"
Write-Host "Watchdog agent API -> http://localhost:8081" -ForegroundColor Green
Start-Sleep -Seconds 3
& '$escapedGo' run ./cmd/agent
"@

Write-Host "Creating psmux session '$SessionName'..." -ForegroundColor Cyan
Invoke-Mux -Executable $mux -Arguments @(
    "new-session", "-d", "-s", $SessionName, "-n", "prometheus",
    "-c", $workspaceRoot, "--",
    $pwsh, "-NoLogo", "-NoExit", "-Command", $prometheusCommand
) | Out-Null

try {
    New-MuxWindow -Executable $mux -Session $SessionName -Name "opencost" -WorkingDirectory $workspaceRoot -Command $openCostCommand
    New-MuxWindow -Executable $mux -Session $SessionName -Name "ai-service" -WorkingDirectory $aiServiceRoot -Command $aiCommand
    New-MuxWindow -Executable $mux -Session $SessionName -Name "agent" -WorkingDirectory $agentRoot -Command $agentCommand
    Invoke-Mux -Executable $mux -Arguments @("select-window", "-t", "${SessionName}:agent") | Out-Null
} catch {
    Invoke-Mux -Executable $mux -Arguments @("kill-session", "-t", $SessionName) -AllowFailure | Out-Null
    throw
}

Write-Host "Watchdog backend is running in psmux session '$SessionName'." -ForegroundColor Green
Write-Host "Windows: prometheus, opencost, ai-service, agent"
Write-Host "Detach: Ctrl+b, then d"
Write-Host "Reattach: psmux attach-session -t $SessionName"
Write-Host "Stop: .\scripts\start-backend.ps1 -Stop"

if (-not $NoAttach) {
    Invoke-Mux -Executable $mux -Arguments @("attach-session", "-t", $SessionName) | Out-Null
}
