# install.ps1 — one-command installer for ftpl (Windows).
#
# Remote (the one-liner):
#   powershell -c "irm https://raw.githubusercontent.com/hnamhocit/ftpl/main/install.ps1 | iex"
#
# Inside a local checkout (skips clone, builds in place):
#   .\install.ps1
$ErrorActionPreference = "Stop"

$repo   = if ($env:FTPL_REPO)         { $env:FTPL_REPO }         else { "https://github.com/hnamhocit/ftpl.git" }
$branch = if ($env:FTPL_BRANCH)       { $env:FTPL_BRANCH }       else { "main" }
$dir    = if ($env:FTPL_INSTALL_DIR)  { $env:FTPL_INSTALL_DIR }  else { Join-Path $HOME ".local\bin" }

foreach ($cmd in @("git", "go")) {
    if (-not (Get-Command $cmd -ErrorAction SilentlyContinue)) {
        throw "$cmd not found. Install it first (git: https://git-scm.com, go: https://go.dev/doc/install)"
    }
}

$tmp = ""
$src = ""
try {
    # If running inside a checkout, build in place, skip clone.
    if ((Test-Path main.go) -and (Test-Path templates)) {
        $src = (Get-Location).Path
        Write-Host "==> Detected ftpl checkout in $src, building in place..."
    }
    else {
        $tmp = Join-Path ([IO.Path]::GetTempPath()) ("ftpl-install-" + [Guid]::NewGuid().ToString("N"))
        New-Item -ItemType Directory -Path $tmp -Force | Out-Null
        Write-Host "==> Cloning $repo ($branch)..."
        git clone --depth 1 --branch $branch $repo (Join-Path $tmp "repo")
        if ($LASTEXITCODE -ne 0) { throw "git clone failed" }
        $src = Join-Path $tmp "repo"
    }

    Push-Location $src
    try {
        Write-Host "==> Building ftpl..."
        go build -o ftpl.exe .
        if ($LASTEXITCODE -ne 0) { throw "go build failed" }
    }
    finally { Pop-Location }

    New-Item -ItemType Directory -Force -Path $dir | Out-Null
    Copy-Item (Join-Path $src "ftpl.exe") (Join-Path $dir "ftpl.exe") -Force
    Write-Host "==> Installed: $dir\ftpl.exe"

    $userPath = [Environment]::GetEnvironmentVariable("Path", "User")
    if ($userPath -notlike "*$dir*") {
        [Environment]::SetEnvironmentVariable("Path", "$userPath;$dir", "User")
        Write-Host "==> Added $dir to user PATH (open a new terminal to apply)"
    }
}
finally {
    # Always clean temp, even on failure.
    if ($tmp -and (Test-Path $tmp)) { Remove-Item -Recurse -Force $tmp }
}

Write-Host ""
Write-Host "Done. Try: ftpl doctor"
