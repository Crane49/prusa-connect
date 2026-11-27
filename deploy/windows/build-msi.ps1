<#
.SYNOPSIS
Builds the Windows installer of Prusa Connect from a built prusa-connect.exe.

.DESCRIPTION
The MSI installs the server as the PrusaConnect service (ADR-20261125-jqp).
The package is deploy/windows/PrusaConnect.wxs, and this script passes it the
version and the binary. It needs WiX 5 and two of its extensions, once per
machine:

  dotnet tool install --global wix --version 5.0.2
  wix extension add --global WixToolset.Util.wixext/5.0.2
  wix extension add --global WixToolset.Firewall.wixext/5.0.2

.PARAMETER Binary
The prusa-connect.exe to install.

.PARAMETER Version
The release, such as 0.1.0-beta.2 or v0.1.0. Windows reads three numbers, so
the leading v and the pre-release suffix are removed.

.PARAMETER Arch
The processor the binary is for: x64 or arm64.

.PARAMETER Output
The MSI to write.

.EXAMPLE
pwsh deploy/windows/build-msi.ps1 -Binary build/prusa-connect.exe -Version 0.1.0-beta.2 -Arch x64 -Output build/PrusaConnect-x64.msi
#>
[CmdletBinding()]
param(
    [Parameter(Mandatory)] [string] $Binary,
    [Parameter(Mandatory)] [string] $Version,
    [ValidateSet('x64', 'arm64')] [string] $Arch = 'x64',
    [Parameter(Mandatory)] [string] $Output
)

$ErrorActionPreference = 'Stop'
Set-StrictMode -Version Latest

if (-not (Test-Path -LiteralPath $Binary -PathType Leaf)) {
    throw "no binary at $Binary"
}

# 0.1.0-beta.2+build.7 is 0.1.0 to Windows. Every pre-release of a release
# shares that version, and the package upgrades across a version it shares.
$numeric = ($Version -replace '^v', '') -replace '[-+].*$', ''
if ($numeric -notmatch '^\d+\.\d+\.\d+$') {
    throw "version $Version does not start with three numbers, such as 0.1.0"
}

if (-not (Get-Command wix -ErrorAction SilentlyContinue)) {
    throw 'wix is not installed; the description of this script names the three commands that install it'
}

$outputFolder = Split-Path -Parent $Output
if ($outputFolder) {
    New-Item -ItemType Directory -Force -Path $outputFolder | Out-Null
}

$arguments = @(
    'build', (Join-Path $PSScriptRoot 'PrusaConnect.wxs'),
    '-arch', $Arch,
    '-ext', 'WixToolset.Util.wixext',
    '-ext', 'WixToolset.Firewall.wixext',
    '-bindpath', $PSScriptRoot,
    '-d', "Version=$numeric",
    '-d', "Binary=$((Resolve-Path -LiteralPath $Binary).Path)",
    '-o', $Output
)

& wix @arguments
if ($LASTEXITCODE -ne 0) {
    throw "wix build failed with exit code $LASTEXITCODE"
}

Write-Host "Built $Output for $Arch, version $numeric"
