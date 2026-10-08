[CmdletBinding()]
param(
    [string]$SessionName = "watchdog",
    [string]$PrometheusService = "prometheus-stack-kube-prom-prometheus",
    [string]$PrometheusNamespace = "monitoring",
    [string]$OpenCostService = "opencost",
    [string]$OpenCostNamespace = "opencost",
    [switch]$Recreate,
    [switch]$NoAttach,
    [switch]$NoDashboard,
    [switch]$SkipDependencyInstall,
    [switch]$Stop
)

# Launches the full Watchdog local stack in one psmux session:
#   prometheus (9090), opencost (9003), ai-service (8000), agent (8081), dashboard (3000)

$ErrorActionPreference = "Stop"
Set-StrictMode -Version Latest
if (Test-Path variable:PSNativeCommandUseErrorActionPreference) {
    $PSNativeCommandUseErrorActionPreference = $false
}

$agentRoot = Split-Path -Parent $PSScriptRoot
$workspaceRoot = Split-Path -Parent $agentRoot
$aiServiceRoot = Join-Path $agentRoot "ai-service"
$dashboardRoot = Join-Path $workspaceRoot "watchdog-dashboard"
$requirementsPath = Join-Path $aiServiceRoot "requirements.txt"
$venvRoot = Join-Path $agentRoot ".venv"
$venvPython = Join-Path $venvRoot "Scripts\python.exe"

function Get-RequiredCommand {
    param([Parameter(Mandatory)][string[]]$Names)

    foreach ($name in $Names) {
        $command = Get-Command $name -CommandType Application -ErrorAction SilentlyContinue | Select-Object -First 1
        if ($command) {
            return $command.Source
        }
    }
    throw "Required command not found: $($Names -join ' or '). Ensure it is installed and on PATH."
}

function Invoke-Mux {
    param([Parameter(Mandatory)][string[]]$Arguments, [switch]$AllowFailure)

    & $script:mux @Arguments
    if (-not $AllowFailure -and $LASTEXITCODE -ne 0) {
        throw "psmux command failed with exit code ${LASTEXITCODE}: $($Arguments -join ' ')"
    }
}

function Test-MuxSession {
    & $script:mux has-session -t $SessionName 2>$null
    return $LASTEXITCODE -eq 0
}

function Assert-PortAvailable {
    param([Parameter(Mandatory)][int]$Port)

    if (Get-NetTCPConnection -State Listen -LocalPort $Port -ErrorAction SilentlyContinue) {
        throw "Port $Port is already in use. Stop the process using it, or run with -Recreate if it is an old Watchdog session."
    }
}

function Assert-KubernetesService {
    param([Parameter(Mandatory)][string]$Namespace, [Parameter(Mandatory)][string]$Service)

    & $script:kubectl get service $Service --namespace $Namespace --output name *> $null
    if ($LASTEXITCODE -ne 0) {
        throw "Kubernetes service '$Service' not found in namespace '$Namespace'. Check: kubectl get svc -n $Namespace"
    }
}

function Quote([string]$Value) {
    return "'" + $Value.Replace("'", "''") + "'"
}

function Add-MuxWindow {
    param(
        [Parameter(Mandatory)][string]$Name,
        [Parameter(Mandatory)][string]$WorkingDirectory,
        [Parameter(Mandatory)][string]$Command,
        [switch]$First
    )

    $target = if ($First) { @("new-session", "-d", "-s", $SessionName) } else { @("new-window", "-d", "-t", $SessionName) }
    Invoke-Mux -Arguments ($target + @(
        "-n", $Name, "-c", $WorkingDirectory, "--",
        $script:pwsh, "-NoLogo", "-NoExit", "-Command", $Command
    ))
}

$mux = Get-RequiredCommand -Names @("psmux", "tmux")

# --- Stop / existing session handling ---------------------------------------

if ($Stop) {
    if (Test-MuxSession) {
        Invoke-Mux -Arguments @("kill-session", "-t", $SessionName)
        Write-Host "Stopped psmux session '$SessionName'." -ForegroundColor Green
    } else {
        Write-Host "No psmux session named '$SessionName' is running." -ForegroundColor Yellow
    }
    return
}

if (Test-MuxSession) {
    if ($Recreate) {
        Invoke-Mux -Arguments @("kill-session", "-t", $SessionName)
        Start-Sleep -Seconds 2
    } else {
        Write-Host "Session '$SessionName' is already running." -ForegroundColor Yellow
        if (-not $NoAttach) {
            Invoke-Mux -Arguments @("attach-session", "-t", $SessionName) -AllowFailure
        }
        return
    }
}

# --- Preflight checks ---------------------------------------------------------

$kubectl = Get-RequiredCommand -Names @("kubectl")
$go = Get-RequiredCommand -Names @("go")
$pwsh = Get-RequiredCommand -Names @("pwsh")
$npm = $null
if (-not $NoDashboard) {
    $npm = Get-RequiredCommand -Names @("npm.cmd", "npm")
    if (-not (Test-Path -LiteralPath $dashboardRoot)) {
        throw "Dashboard directory not found at '$dashboardRoot'. Run with -NoDashboard to skip it."
    }
}

Write-Host "Checking Kubernetes access..." -ForegroundColor Cyan
& $kubectl cluster-info *> $null
if ($LASTEXITCODE -ne 0) {
    throw "The current Kubernetes context is unavailable. Check 'kubectl config current-context' and 'kubectl cluster-info'."
}
Assert-KubernetesService -Namespace $PrometheusNamespace -Service $PrometheusService
Assert-KubernetesService -Namespace $OpenCostNamespace -Service $OpenCostService

$ports = @(9090, 9003, 8000, 8081)
if (-not $NoDashboard) { $ports += 3000 }
foreach ($port in $ports) { Assert-PortAvailable -Port $port }

if (-not (Test-Path -LiteralPath $venvPython)) {
    throw "Python virtual environment not found. Expected interpreter: '$venvPython'."
}

& $venvPython -c "import fastapi, uvicorn, langgraph, langchain, pandas, pydantic" 2>$null
if ($LASTEXITCODE -ne 0) {
    if ($SkipDependencyInstall) {
        throw "AI service dependencies are missing. Rerun without -SkipDependencyInstall."
    }
    Write-Host "Installing AI service dependencies..." -ForegroundColor Cyan
    & $venvPython -m pip install -r $requirementsPath
    if ($LASTEXITCODE -ne 0) { throw "Failed to install AI service dependencies." }
}

# --- Window commands ----------------------------------------------------------

$prometheusCommand = "Write-Host 'Prometheus -> http://localhost:9090' -ForegroundColor Green; & $(Quote $kubectl) port-forward svc/$PrometheusService -n $PrometheusNamespace 9090:9090"
$openCostCommand = "Write-Host 'OpenCost -> http://localhost:9003' -ForegroundColor Green; & $(Quote $kubectl) port-forward svc/$OpenCostService -n $OpenCostNamespace 9003:9003"
$aiCommand = "Write-Host 'AI service -> http://localhost:8000' -ForegroundColor Green; & $(Quote $venvPython) -m uvicorn main:app --host 127.0.0.1 --port 8000 --reload"
$agentCommand = @(
    '$env:WATCHDOG_PROMETHEUS_URL = ''http://localhost:9090'''
    '$env:WATCHDOG_OPENCOST_URL = ''http://localhost:9003'''
    '$env:WATCHDOG_AI_SERVICE_URL = ''http://localhost:8000'''
    '$env:WATCHDOG_STORAGE_PATH = ''./data.db'''
    '$env:WATCHDOG_API_ALLOWED_ORIGINS = ''http://localhost:3000'''
    "Write-Host 'Watchdog agent API -> http://localhost:8081' -ForegroundColor Green"
    'Start-Sleep -Seconds 3'
    "& $(Quote $go) run ./cmd/agent"
) -join '; '

# --- Launch -------------------------------------------------------------------

Write-Host "Creating psmux session '$SessionName'..." -ForegroundColor Cyan
Add-MuxWindow -First -Name "prometheus" -WorkingDirectory $workspaceRoot -Command $prometheusCommand

try {
    Add-MuxWindow -Name "opencost" -WorkingDirectory $workspaceRoot -Command $openCostCommand
    Add-MuxWindow -Name "ai-service" -WorkingDirectory $aiServiceRoot -Command $aiCommand
    Add-MuxWindow -Name "agent" -WorkingDirectory $agentRoot -Command $agentCommand

    $windows = "prometheus, opencost, ai-service, agent"
    if (-not $NoDashboard) {
        $dashboardCommand = @(
            "if (-not (Test-Path node_modules)) { & $(Quote $npm) ci }"
            "Write-Host 'Dashboard -> http://localhost:3000' -ForegroundColor Green"
            'Start-Sleep -Seconds 5'
            "& $(Quote $npm) run dev"
        ) -join '; '
        Add-MuxWindow -Name "dashboard" -WorkingDirectory $dashboardRoot -Command $dashboardCommand
        $windows += ", dashboard"
    }

    Invoke-Mux -Arguments @("select-window", "-t", "${SessionName}:agent")
} catch {
    Invoke-Mux -Arguments @("kill-session", "-t", $SessionName) -AllowFailure
    throw
}

Write-Host "Watchdog is running in psmux session '$SessionName'." -ForegroundColor Green
Write-Host "Windows:   $windows"
Write-Host "Switch:    Ctrl+b then n / p (or 0-4)"
Write-Host "Detach:    Ctrl+b then d"
Write-Host "Reattach:  psmux attach -t $SessionName"
Write-Host "Stop:      .\scripts\start-backend.ps1 -Stop"

if (-not $NoAttach) {
    Invoke-Mux -Arguments @("attach-session", "-t", $SessionName) -AllowFailure
}
