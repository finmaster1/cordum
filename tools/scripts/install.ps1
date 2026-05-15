# install.ps1 — EDGE-151 binary integrity pre-activation gate (PowerShell
# mirror of tools/scripts/install.sh). Unlike install.sh this script does
# NOT include the docker-compose orchestrator path; Windows users run the
# platform install via WSL + install.sh. install.ps1 is purpose-built for
# verifying and activating signed cordum-hook/cordum-agentd/cordum-claude
# binaries on Windows endpoints.
#
# Usage:
#   pwsh -NoProfile -File install.ps1 -ReleaseDir <dir> [-DevAllowUnsigned] [-InstallDir <dir>]
#
# See docs/security/binary-signing.md for the threat model.

#Requires -Version 5.1
[CmdletBinding()]
param(
    [Parameter(Mandatory = $true)]
    [string]$ReleaseDir,
    [switch]$DevAllowUnsigned,
    [string]$InstallDir = $env:CORDUM_INSTALL_DIR,
    [string]$GpgExe = 'gpg.exe'
)

$ErrorActionPreference = 'Stop'
$ProdFingerprintPin = $env:CORDUM_RELEASE_FINGERPRINT

# Audit-log context — see docs/security/binary-signing.md §8 for the
# stable schema. Empty values are emitted as "".
$script:AuditSigScheme = ''
$script:AuditFingerprint = ''

function Emit-Audit {
    param(
        [string]$Event,
        [string]$Reason = '',
        [string]$Hash = '',
        [string]$Rel = '',
        [int]$ExitCode = 0
    )
    $payload = [ordered]@{
        event       = $Event
        hash        = $Hash
        path        = $Rel
        sig_scheme  = $script:AuditSigScheme
        fingerprint = $script:AuditFingerprint
        reason      = $Reason
        exit_code   = $ExitCode
    }
    [Console]::Error.WriteLine(($payload | ConvertTo-Json -Compress))
}

function Verify-Fail {
    param([string]$Reason)
    Emit-Audit -Event 'binary-verify-fail' -Reason $Reason -ExitCode 1
    [Console]::Error.WriteLine("BINARY-VERIFY-FAIL: $Reason")
    exit 1
}

function Normalise-Fpr {
    param([string]$Value)
    return (($Value -replace '[\s\r\n]', '')).ToUpperInvariant()
}

function Reject-Path {
    param([string]$P)
    if ($P -match '^[/\\]') { return $true }
    if ($P -match '^[A-Za-z]:') { return $true }
    $forward = $P -replace '\\', '/'
    if ($forward -match '(^|/)\.\.(/|$)') { return $true }
    return $false
}

function Invoke-BinaryVerify {
    param(
        [string]$ReleaseDir,
        [bool]$Dev,
        [string]$InstallTo
    )

    if (-not (Test-Path -LiteralPath $ReleaseDir -PathType Container)) {
        Verify-Fail "release-dir not found: $ReleaseDir"
    }
    $manifest = Join-Path $ReleaseDir 'SHA256SUMS'
    $sig = Join-Path $ReleaseDir 'SHA256SUMS.asc'
    if (-not (Test-Path -LiteralPath $manifest)) { Verify-Fail 'manifest not found' }
    if (-not (Test-Path -LiteralPath $sig)) { Verify-Fail 'unsigned manifest' }

    $gpg = Get-Command $GpgExe -ErrorAction SilentlyContinue
    if (-not $gpg) {
        $gpg = Get-Command 'gpg' -ErrorAction SilentlyContinue
    }
    if (-not $gpg) { Verify-Fail 'gpg required for signature verification' }

    # $PSScriptRoot is the canonical "directory of the running script" in
    # PowerShell 5.1+; $MyInvocation.MyCommand.Path is unset when the
    # script is invoked via `-File` so we must not rely on it.
    $scriptDir = $PSScriptRoot
    $repoRoot = Resolve-Path (Join-Path $scriptDir '..\..')

    if ($Dev) {
        $pubkey = Join-Path $repoRoot 'tools\test-keys\TEST-ONLY-release.pub.asc'
        if (-not (Test-Path -LiteralPath $pubkey)) {
            Verify-Fail "dev mode but TEST-ONLY pubkey missing at $pubkey"
        }
        $script:AuditSigScheme = 'dev'
    } else {
        $pubkey = Join-Path $repoRoot 'tools\keys\cordum-release.pub.asc'
        if (-not (Test-Path -LiteralPath $pubkey)) {
            Verify-Fail 'production release pubkey not provisioned at tools/keys/cordum-release.pub.asc; pass -DevAllowUnsigned for TEST-ONLY mode'
        }
        if (-not $ProdFingerprintPin) {
            Verify-Fail 'production release fingerprint not provisioned (set $env:CORDUM_RELEASE_FINGERPRINT)'
        }
        $script:AuditSigScheme = 'gpg'
    }

    $tmpHome = New-Item -ItemType Directory -Path ([IO.Path]::Combine([IO.Path]::GetTempPath(), [Guid]::NewGuid().ToString('N')))
    try {
        & $gpg.Source --homedir $tmpHome.FullName --batch --quiet --import $pubkey 2>$null | Out-Null
        if ($LASTEXITCODE -ne 0) { Verify-Fail "gpg --import failed for $pubkey" }

        $colonLines = & $gpg.Source --homedir $tmpHome.FullName --with-colons --list-keys 2>$null
        $importedFpr = $null
        foreach ($cl in $colonLines) {
            if ($cl -match '^fpr:::::::::([0-9A-F]+):') {
                $importedFpr = $matches[1]
                break
            }
        }
        if (-not $importedFpr) { Verify-Fail 'failed to extract fingerprint from pubkey' }
        $script:AuditFingerprint = $importedFpr

        if ($Dev) {
            $uidHasMarker = $false
            foreach ($cl in $colonLines) {
                if ($cl -match '^uid:.*TEST-ONLY') { $uidHasMarker = $true; break }
            }
            if (-not $uidHasMarker) {
                Verify-Fail 'dev mode but imported pubkey UID lacks TEST-ONLY marker'
            }
            if ($ProdFingerprintPin) {
                $g = Normalise-Fpr $importedFpr
                $w = Normalise-Fpr $ProdFingerprintPin
                if ($g -eq $w) { Verify-Fail '-DevAllowUnsigned refuses production fingerprint' }
            }
        } else {
            $g = Normalise-Fpr $importedFpr
            $w = Normalise-Fpr $ProdFingerprintPin
            if ($g -ne $w) {
                Verify-Fail "release pubkey fingerprint $g does not match pinned $w"
            }
        }

        & $gpg.Source --homedir $tmpHome.FullName --batch --quiet --verify $sig $manifest 2>$null
        if ($LASTEXITCODE -ne 0) { Verify-Fail 'gpg signature invalid' }

        $lines = Get-Content -LiteralPath $manifest
        $entries = New-Object System.Collections.Generic.List[hashtable]
        foreach ($raw in $lines) {
            $line = $raw.TrimEnd("`r")
            if ([string]::IsNullOrWhiteSpace($line)) { continue }
            $parts = $line -split ' ', 2
            if ($parts.Count -lt 2) { Verify-Fail "malformed manifest line: $line" }
            $hash = $parts[0]
            $rel = $parts[1].TrimStart(@(' ', '*'))
            if ([string]::IsNullOrEmpty($hash) -or [string]::IsNullOrEmpty($rel)) {
                Verify-Fail "malformed manifest line: $line"
            }
            if (Reject-Path $rel) { Verify-Fail "manifest path traversal: $rel" }
            $target = Join-Path $ReleaseDir $rel
            if (-not (Test-Path -LiteralPath $target)) { Verify-Fail "binary missing $rel" }
            $got = (Get-FileHash -LiteralPath $target -Algorithm SHA256).Hash.ToLower()
            if ($got -ne $hash.ToLower()) { Verify-Fail "hash mismatch $rel" }

            # Get-AuthenticodeSignature is a Windows-only convenience check.
            # Status `NotSigned` is permissible for Tier 1-only (GPG-signed
            # manifest); Tier 2 enforcement against `Valid` happens on the
            # Windows runner via binaries-pr-validation.yml when the
            # workflow's signtool step has executed.
            if ($target -match '\.exe$' -and (Get-Command 'Get-AuthenticodeSignature' -ErrorAction SilentlyContinue)) {
                $auth = Get-AuthenticodeSignature -LiteralPath $target
                if ($auth.Status -ne 'NotSigned' -and $auth.Status -ne 'Valid') {
                    Verify-Fail "authenticode verify failed $rel (status=$($auth.Status))"
                }
                if ($auth.Status -eq 'Valid') {
                    $prevScheme = $script:AuditSigScheme
                    $script:AuditSigScheme = 'authenticode'
                    Emit-Audit -Event 'binary-verify-ok' -Hash $hash -Rel $rel
                    $script:AuditSigScheme = $prevScheme
                }
            }
            Emit-Audit -Event 'binary-verify-ok' -Hash $hash -Rel $rel
            $entries.Add(@{ Hash = $hash; Rel = $rel; Target = $target }) | Out-Null
        }

        if ($InstallTo) {
            New-Item -ItemType Directory -Path $InstallTo -Force | Out-Null
            foreach ($e in $entries) {
                $dst = Join-Path $InstallTo $e.Rel
                New-Item -ItemType Directory -Path (Split-Path -Parent $dst) -Force | Out-Null
                Move-Item -LiteralPath $e.Target -Destination $dst -Force
                # Recompute SHA-256 AFTER the move — defence-in-depth
                # against sig-then-swap race.
                $post = (Get-FileHash -LiteralPath $dst -Algorithm SHA256).Hash.ToLower()
                if ($post -ne $e.Hash.ToLower()) {
                    Verify-Fail "post-activation hash mismatch $($e.Rel)"
                }
                Emit-Audit -Event 'binary-verify-ok' -Hash $post -Rel $e.Rel
            }
            Write-Host "[install] activated $($entries.Count) binaries under $InstallTo"
        } else {
            Write-Host "[install] release-dir verified: $ReleaseDir ($($entries.Count) binaries match manifest)"
        }
    } finally {
        Remove-Item -Recurse -Force -LiteralPath $tmpHome.FullName -ErrorAction SilentlyContinue
    }
}

Invoke-BinaryVerify -ReleaseDir $ReleaseDir -Dev ($DevAllowUnsigned.IsPresent) -InstallTo $InstallDir
exit 0
