# install_test.ps1 — synthetic-tampered + unsigned + valid scenarios for
# install.ps1.
#
# EDGE-151 reopen #2 RED: covers Windows/MSYS path-conversion fix. Fixture
# signing/import errors are HARD failures with captured gpg diagnostics
# (never silently fall through as "unsigned manifest"). Three assertions:
#   (1) valid synthetic TEST-ONLY release verifies OK through install.ps1
#   (2) signed-then-tampered cordum-hook fails with
#       BINARY-VERIFY-FAIL: hash mismatch cordum-hook
#   (3) removing SHA256SUMS.asc fails with
#       BINARY-VERIFY-FAIL: unsigned manifest
# The script accepts -GpgExe <path> so CI can target Git/MSYS gpg
# (`C:\Program Files\Git\usr\bin\gpg.exe`) and Gpg4Win/native gpg
# independently.

#Requires -Version 5.1
[CmdletBinding()]
param(
    [string]$GpgExe = 'gpg.exe'
)

$ErrorActionPreference = 'Stop'
$scriptDir = $PSScriptRoot
$repoRoot = Resolve-Path (Join-Path $scriptDir '..\..')
$installPs1 = Join-Path $scriptDir 'install.ps1'
$testKeysDir = Join-Path $repoRoot 'tools\test-keys'
$privKey = Join-Path $testKeysDir 'TEST-ONLY-release.priv.asc'

. (Join-Path $scriptDir 'windows\gpg-path.ps1')

try {
    $gpgCmd = Resolve-CordumGpgCommand -GpgExe $GpgExe
} catch {
    Write-Error "install_test.ps1: $_"
    exit 2
}
if (-not (Test-Path -LiteralPath $privKey)) {
    Write-Error "install_test.ps1: missing $privKey -- run tools/test-keys/gen.sh"
    exit 2
}
$gpgMode = Get-CordumGpgPathMode -GpgCommand $gpgCmd

$work = New-Item -ItemType Directory -Path ([System.IO.Path]::Combine([System.IO.Path]::GetTempPath(), [System.Guid]::NewGuid().ToString('N')))
try {
    $gpgHome = Join-Path $work.FullName 'gpg'
    New-Item -ItemType Directory -Path $gpgHome | Out-Null

    $gpgHomeArg = ConvertTo-CordumGpgArgPath -Path $gpgHome -Mode $gpgMode
    $privKeyArg = ConvertTo-CordumGpgArgPath -Path $privKey -Mode $gpgMode

    $importOut = & $gpgCmd.Source --homedir $gpgHomeArg --batch --quiet --import $privKeyArg 2>&1
    if ($LASTEXITCODE -ne 0) {
        $diag = ($importOut | Out-String).Trim()
        throw "install_test.ps1: gpg --import of TEST-ONLY private key failed (exit=$LASTEXITCODE)`n$diag"
    }

    function Build-ReleaseDir {
        param([string]$Dest)
        New-Item -ItemType Directory -Path $Dest | Out-Null
        $binPath = Join-Path $Dest 'cordum-hook'
        Set-Content -LiteralPath $binPath -Value 'placeholder' -NoNewline -Encoding ASCII
        $hash = (Get-FileHash -LiteralPath $binPath -Algorithm SHA256).Hash.ToLower()
        $manifestLine = "$hash  cordum-hook"
        $manifestPath = Join-Path $Dest 'SHA256SUMS'
        Set-Content -LiteralPath $manifestPath -Value $manifestLine -NoNewline -Encoding ASCII
        $sigPath = Join-Path $Dest 'SHA256SUMS.asc'
        $manifestArg = ConvertTo-CordumGpgArgPath -Path $manifestPath -Mode $gpgMode
        $sigArg      = ConvertTo-CordumGpgArgPath -Path $sigPath      -Mode $gpgMode
        $signOut = & $gpgCmd.Source --homedir $gpgHomeArg --batch --yes --quiet --detach-sign --armor `
            --output $sigArg $manifestArg 2>&1
        if ($LASTEXITCODE -ne 0) {
            $diag = ($signOut | Out-String).Trim()
            throw "install_test.ps1: gpg --detach-sign failed for $Dest (exit=$LASTEXITCODE)`n$diag"
        }
        if (-not (Test-Path -LiteralPath $sigPath)) {
            throw "install_test.ps1: --output $sigPath was not produced by gpg --detach-sign"
        }
    }

    function Invoke-Install {
        param([string]$ReleaseDir)
        return (& pwsh -NoProfile -File $installPs1 `
            -DevAllowUnsigned `
            -ReleaseDir $ReleaseDir `
            -GpgExe $GpgExe 2>&1)
    }

    function Expect-VerifyFail {
        param([string]$Label, [string]$Needle, [string]$ReleaseDir)
        if (-not (Test-Path -LiteralPath $installPs1)) {
            Write-Host "FAIL [$Label]: install.ps1 missing" -ForegroundColor Red
            return $false
        }
        $out = Invoke-Install -ReleaseDir $ReleaseDir
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

    function Expect-VerifyOk {
        param([string]$Label, [string]$ReleaseDir)
        if (-not (Test-Path -LiteralPath $installPs1)) {
            Write-Host "FAIL [$Label]: install.ps1 missing" -ForegroundColor Red
            return $false
        }
        $out = Invoke-Install -ReleaseDir $ReleaseDir
        $rc = $LASTEXITCODE
        if ($rc -ne 0) {
            Write-Host "FAIL [$Label]: install.ps1 exit=$rc; expected 0" -ForegroundColor Red
            $out | ForEach-Object { Write-Host $_ }
            return $false
        }
        Write-Host "PASS [$Label]: install.ps1 verified $ReleaseDir" -ForegroundColor Green
        return $true
    }

    $failures = 0

    # Scenario 1 — valid synthetic release: install.ps1 must verify cleanly.
    $valid = Join-Path $work.FullName 'release-valid'
    Build-ReleaseDir $valid
    if (-not (Expect-VerifyOk 'valid-release' $valid)) { $failures++ }

    # Scenario 2 — tampered binary: post-sign mutation must trigger the
    # SHA-256 comparison inside install.ps1's pre-activation gate.
    $tampered = Join-Path $work.FullName 'release-tampered'
    Build-ReleaseDir $tampered
    Add-Content -LiteralPath (Join-Path $tampered 'cordum-hook') -Value 'tamper' -NoNewline
    if (-not (Expect-VerifyFail 'tampered-binary' 'BINARY-VERIFY-FAIL: hash mismatch cordum-hook' $tampered)) {
        $failures++
    }

    # Scenario 3 — unsigned manifest: SHA256SUMS.asc removed; install must
    # abort even though SHA256SUMS itself is intact.
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
