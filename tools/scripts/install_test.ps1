# install_test.ps1 — synthetic-tampered + unsigned scenarios for install.ps1.
#
# EDGE-151 step-2 RED: install.ps1 does not exist yet (created in step-5).
# Once it lands with the pre-activation gate, both scenarios MUST exit nonzero
# with a typed BINARY-VERIFY-FAIL message. CI (Windows runner) wires this up
# in step-4 binaries-pr-validation.yml.

#Requires -Version 5.1
[CmdletBinding()]
param(
    [string]$GpgExe = 'gpg.exe'
)

$ErrorActionPreference = 'Stop'
$scriptDir = Split-Path -Parent $MyInvocation.MyCommand.Path
$repoRoot = Resolve-Path (Join-Path $scriptDir '..\..')
$installPs1 = Join-Path $scriptDir 'install.ps1'
$testKeysDir = Join-Path $repoRoot 'tools\test-keys'
$privKey = Join-Path $testKeysDir 'TEST-ONLY-release.priv.asc'

if (-not (Get-Command $GpgExe -ErrorAction SilentlyContinue)) {
    Write-Error "install_test.ps1: $GpgExe required for synthetic-signing fixtures"
    exit 2
}
if (-not (Test-Path -LiteralPath $privKey)) {
    Write-Error "install_test.ps1: missing $privKey — run tools/test-keys/gen.sh"
    exit 2
}

$work = New-Item -ItemType Directory -Path ([System.IO.Path]::Combine([System.IO.Path]::GetTempPath(), [System.Guid]::NewGuid().ToString('N')))
try {
    $gpgHome = Join-Path $work.FullName 'gpg'
    New-Item -ItemType Directory -Path $gpgHome | Out-Null
    & $GpgExe --homedir $gpgHome --batch --quiet --import $privKey | Out-Null

    function Build-ReleaseDir {
        param([string]$Dest)
        New-Item -ItemType Directory -Path $Dest | Out-Null
        $binPath = Join-Path $Dest 'cordum-hook.exe'
        Set-Content -LiteralPath $binPath -Value 'placeholder' -NoNewline -Encoding ASCII
        $hash = (Get-FileHash -LiteralPath $binPath -Algorithm SHA256).Hash.ToLower()
        $manifestLine = "$hash  cordum-hook.exe"
        Set-Content -LiteralPath (Join-Path $Dest 'SHA256SUMS') -Value $manifestLine -NoNewline -Encoding ASCII
        & $GpgExe --homedir $gpgHome --batch --yes --quiet --detach-sign --armor `
            --output (Join-Path $Dest 'SHA256SUMS.asc') (Join-Path $Dest 'SHA256SUMS') | Out-Null
    }

    function Expect-VerifyFail {
        param([string]$Label, [string]$Needle, [string]$ReleaseDir)
        if (-not (Test-Path -LiteralPath $installPs1)) {
            Write-Host "FAIL [$Label]: install.ps1 missing (expected step-5)" -ForegroundColor Red
            return $false
        }
        $out = & pwsh -NoProfile -File $installPs1 --dev-allow-unsigned --release-dir $ReleaseDir 2>&1
        $rc = $LASTEXITCODE
        if ($rc -eq 0) {
            Write-Host "FAIL [$Label]: install.ps1 exit=0; expected nonzero" -ForegroundColor Red
            $out | ForEach-Object { Write-Host $_ }
            return $false
        }
        if (-not ($out -match [regex]::Escape($Needle))) {
            Write-Host "FAIL [$Label]: stderr missing '$Needle'" -ForegroundColor Red
            $out | ForEach-Object { Write-Host $_ }
            return $false
        }
        Write-Host "PASS [$Label]: install.ps1 refused with '$Needle'" -ForegroundColor Green
        return $true
    }

    $failures = 0

    # Scenario 1 — tampered binary
    $tampered = Join-Path $work.FullName 'release-tampered'
    Build-ReleaseDir $tampered
    Add-Content -LiteralPath (Join-Path $tampered 'cordum-hook.exe') -Value 'tamper' -NoNewline
    if (-not (Expect-VerifyFail 'tampered-binary' 'BINARY-VERIFY-FAIL: hash mismatch cordum-hook' $tampered)) {
        $failures++
    }

    # Scenario 2 — unsigned manifest
    $unsigned = Join-Path $work.FullName 'release-unsigned'
    Build-ReleaseDir $unsigned
    Remove-Item -LiteralPath (Join-Path $unsigned 'SHA256SUMS.asc') -Force
    if (-not (Expect-VerifyFail 'unsigned-manifest' 'BINARY-VERIFY-FAIL: unsigned manifest' $unsigned)) {
        $failures++
    }

    if ($failures -ne 0) {
        Write-Error "install_test.ps1: $failures scenario(s) failed"
        exit 1
    }
    Write-Host 'install_test.ps1: all synthetic verification scenarios passed'
}
finally {
    Remove-Item -Recurse -Force -LiteralPath $work.FullName -ErrorAction SilentlyContinue
}
