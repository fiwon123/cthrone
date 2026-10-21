#Requires -Version 5.1
<#
.SYNOPSIS
    Windows-native entry point for the dev sandbox

.DESCRIPTION
    `make dev-up` cannot run on native Windows: the Makefile's preflight and
    recipes use POSIX commands (`test -x`, `stat`, `mkdir -p`, `id -u`) and
    `make` invokes cmd.exe/PowerShell rather than a POSIX shell. This wrapper
    runs the cross-platform Python launcher instead, so the same developer
    workflows are available from PowerShell without installing WSL.

    Demo users do not need this: run `docker compose up` in the repository root
    instead.

.EXAMPLE
    .\scripts\dev-up.ps1 dev-up

.EXAMPLE
    .\scripts\dev-up.ps1 opencode

.NOTES
    The in-container AI agent needs a Linux opencode binary, which only exists
    on Linux/WSL2. On native Windows, run `opencode` on the host against the
    running stack.
#>
[CmdletBinding()]
param(
    [Parameter(ValueFromRemainingArguments = $true)]
    [string[]] $CommandArgs
)

$ErrorActionPreference = 'Stop'

# Make UTF-8 output from Python render correctly in the Windows console.
try {
    [Console]::OutputEncoding = [System.Text.Encoding]::UTF8
} catch {
    # Older consoles may reject the change; it is cosmetic only.
}

$scriptPath = Join-Path $PSScriptRoot 'dev-up.py'
if (-not (Test-Path -LiteralPath $scriptPath)) {
    Write-Error "dev-up.py not found next to this script: $scriptPath"
    exit 1
}

$python = Get-Command python -ErrorAction SilentlyContinue
if ($null -ne $python) {
    & $python.Source $scriptPath @CommandArgs
    exit $LASTEXITCODE
}

# The Microsoft Store / python.org installers ship the `py` launcher but not
# always a `python` shim on PATH.
$pyLauncher = Get-Command py -ErrorAction SilentlyContinue
if ($null -ne $pyLauncher) {
    & $pyLauncher.Source -3 $scriptPath @CommandArgs
    exit $LASTEXITCODE
}

Write-Error @'
Python 3.10+ is required to run the Windows dev-sandbox helper.

Install it with one of:
  winget install --id Python.Python.3.12
  https://www.python.org/downloads/windows/

Alternatively, use WSL2 (recommended for this project) and run `make dev-up`,
or run `docker compose up` for a demo of the project.
'@
exit 1
