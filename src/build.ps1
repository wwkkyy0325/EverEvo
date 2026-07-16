param([string]$Target = "all")

$ErrorActionPreference = "Stop"
$Root = Split-Path -Parent $MyInvocation.MyCommand.Path
Set-Location $Root

$env:Path = "$env:USERPROFILE\.cargo\bin;F:\go\bin;$env:Path"
$Bin = Join-Path $Root "..\dist\bin"

function Kill-Running { Get-Process everevo -ErrorAction SilentlyContinue | Stop-Process -Force -ErrorAction SilentlyContinue }

function Build-Frontend {
    Write-Host "==> Frontend" -ForegroundColor Cyan
    Push-Location frontend
    npm install
    npm run build
    Pop-Location
}

function Build-App {
    Write-Host "==> Go build" -ForegroundColor Cyan
    Kill-Running
    wails build -s
}

function Show-Help {
    Write-Host "Usage: .\build.ps1 [target]"
    Write-Host "  all       full build: frontend + EXE"
    Write-Host "  frontend  build frontend only"
    Write-Host "  dev       dev mode (hot reload + logs)"
    Write-Host "  run       run the EXE"
    Write-Host "  package   full build + zip"
    Write-Host "  clean     remove all build artifacts"
}

switch ($Target) {
    "frontend" { Build-Frontend }
    "build"    { Build-Frontend; Build-App }
    "all"      { Build-Frontend; Build-App }
    "dev"      { wails dev }
    "run"      {
        $exe = Join-Path $Bin "everevo.exe"
        if (-not (Test-Path $exe)) { Write-Host "EXE not found, run .\build.ps1 all first"; exit 1 }
        & $exe
    }
    "package"  {
        Build-Frontend; Build-App
        $distDir = Join-Path $Root "..\dist"
        New-Item -ItemType Directory -Force -Path $distDir | Out-Null
        $zip = Join-Path $distDir "EverEvo-v0.1.0-Windows.zip"
        Compress-Archive -Path "$Bin\everevo.exe" -DestinationPath $zip -Force
        Write-Host "==> Package: $zip" -ForegroundColor Green
    }
    "clean"    {
        Remove-Item -Recurse -Force "frontend\dist" -ErrorAction SilentlyContinue
        Remove-Item -Recurse -Force $Bin -ErrorAction SilentlyContinue
        Write-Host "==> Cleaned" -ForegroundColor Green
    }
    default    { Show-Help }
}

if ($Target -in @("all","build","package")) {
    Write-Host ""
    Write-Host "==> Done" -ForegroundColor Green
    Write-Host "EXE:  $Bin\everevo.exe"
    Write-Host "Run:  .\build.ps1 run"
}
