<#
.SYNOPSIS
  Runs install.ps1's own member rules (Expand-GuiArchive, T-70 IC-4) against a Windows GUI archive.

.DESCRIPTION
  Used by scripts/check-gui-archives.sh in CI (PowerShell 7 on Linux). The functions and the notice list are read from
  install.ps1 itself, never copied, so the check follows the installer. Only the member rules are exercised here (names,
  types, the fixed notice list, mothdex-gui.exe and SDL2.dll present); folder ACLs and the probe are Windows behavior
  covered by tests/installer/install.Tests.ps1.
.PARAMETER Installer
  Path of install.ps1.
.PARAMETER Zip
  The mothdex-gui_<version>_windows_amd64.zip to check.
.PARAMETER Notices
  The file names of THIRD_PARTY/, comma separated: the archive must carry exactly these notices.
#>
param(
  [Parameter(Mandatory)][string]$Installer,
  [Parameter(Mandatory)][string]$Zip,
  [Parameter(Mandatory)][string]$Notices
)
Set-StrictMode -Version 2.0
$ErrorActionPreference = 'Stop'

$errors = $null
$ast = [System.Management.Automation.Language.Parser]::ParseFile((Resolve-Path $Installer).Path, [ref]$null, [ref]$errors)
if ($errors.Count -gt 0) { throw "install.ps1 does not parse: $($errors[0].Message)" }

$wanted = @('Fail', 'Test-SafeName', 'Expand-GuiArchive')
$functions = $ast.FindAll({ param($node) ($node -is [System.Management.Automation.Language.FunctionDefinitionAst]) -and ($wanted -contains $node.Name) }, $true)
if (@($functions).Count -ne $wanted.Count) { throw "could not find $($wanted -join ', ') in install.ps1" }
foreach ($function in $functions) { Invoke-Expression $function.Extent.Text }
$assignment = $ast.FindAll({ param($node) ($node -is [System.Management.Automation.Language.AssignmentStatementAst]) -and ($node.Left.Extent.Text -eq '$NoticeNames') }, $true) | Select-Object -First 1
if (-not $assignment) { throw 'could not find $NoticeNames in install.ps1' }
Invoke-Expression $assignment.Extent.Text

$dest = Join-Path ([IO.Path]::GetTempPath()) ('gui-zip-' + [Guid]::NewGuid().ToString('N'))
New-Item -ItemType Directory -Path $dest | Out-Null
try {
  $found = @(Expand-GuiArchive (Resolve-Path $Zip).Path (Split-Path -Leaf $Zip) $dest) | Sort-Object
  $want = @($Notices -split ',' | Where-Object { $_ } | Sort-Object)
  if (($found -join ',') -ne ($want -join ',')) { throw "notices in the archive: $($found -join ', '); THIRD_PARTY/ has: $($want -join ', ')" }
  foreach ($name in 'mothdex-gui.exe', 'SDL2.dll') {
    if (-not (Test-Path -LiteralPath (Join-Path $dest $name) -PathType Leaf)) { throw "$name was not extracted" }
  }
  Write-Output "install.ps1 accepts $(Split-Path -Leaf $Zip): $($found.Count) notices, mothdex-gui.exe and SDL2.dll"
} finally {
  Remove-Item -LiteralPath $dest -Recurse -Force -ErrorAction SilentlyContinue
}
