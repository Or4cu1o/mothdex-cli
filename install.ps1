<#
.SYNOPSIS
  Mothdex installer for Windows and RetroBat (Windows PowerShell 5.1 or PowerShell 7).

.DESCRIPTION
  Download this file, read it, then run it. Do not pipe it into Invoke-Expression:
    Invoke-WebRequest https://raw.githubusercontent.com/Or4cu1o/mothdex-cli/main/install.ps1 -OutFile install.ps1
    .\install.ps1 -DryRun

  It downloads only from https://github.com/Or4cu1o/mothdex-cli/releases (the base URL is fixed in this file), checks
  the zip's SHA-256 against checksums.txt of the same release (always), verifies the cosign signature of checksums.txt
  when cosign is installed (or `gh attestation verify` when gh is installed and logged in), and installs mothdex.exe.
  Default location: %LocalAppData%\Programs\mothdex (per-user, not writable by other users). With RetroBat found, a
  launcher (Mothdex.bat) is written to <RetroBat>\roms\windows. See docs/contracts/cli/release-artifacts.md.

.PARAMETER Version
  Release to install (X.Y.Z or vX.Y.Z; pre-releases only by naming them). Default: the latest non-pre-release.
.PARAMETER RetroBatPath
  RetroBat folder (contains RetroBat.exe and retrobat.ini). Default: C:\RetroBat when RetroBat.exe is there.
.PARAMETER BinDir
  Install here instead of %LocalAppData%\Programs\mothdex. A folder outside %LocalAppData% gets an explicit ACL
  (current user, SYSTEM and Administrators full control; Users read and execute).
.PARAMETER NoLauncher
  Do not create the RetroBat launcher.
.PARAMETER DryRun
  Download and verify, print what would be installed, change nothing.
.PARAMETER Uninstall
  Remove mothdex.exe and the launcher created by this installer.
#>
[CmdletBinding()]
param(
  [string]$Version = '',
  [string]$RetroBatPath = '',
  [string]$BinDir = '',
  [switch]$NoLauncher,
  [switch]$DryRun,
  [switch]$Uninstall
)

Set-StrictMode -Version 2.0
$ErrorActionPreference = 'Stop'
$ProgressPreference = 'SilentlyContinue'

$Repo = 'Or4cu1o/mothdex-cli'
$Base = "https://github.com/$Repo/releases"
$SignerIdentity = "https://github.com/$Repo/.github/workflows/release.yml@refs/tags/"
$OidcIssuer = 'https://token.actions.githubusercontent.com'
$Marker = 'rem mothdex-launcher v1'
$LauncherName = 'Mothdex.bat'

function Fail([string]$Message) { throw "mothdex install: $Message" }

# Only URLs built from $Base are ever downloaded.
function Get-Remote([string]$Url, [string]$Dest) {
  if (-not $Url.StartsWith("$Base/", [StringComparison]::Ordinal)) { Fail "refusing to download $Url" }
  try {
    [Net.ServicePointManager]::SecurityProtocol = [Net.ServicePointManager]::SecurityProtocol -bor [Net.SecurityProtocolType]::Tls12
  } catch { }
  Invoke-WebRequest -Uri $Url -OutFile $Dest -UseBasicParsing -MaximumRedirection 5 | Out-Null
}

function Test-Version([string]$Value) { return $Value -match '^[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z][0-9A-Za-z.]*)?$' }

# Absolute drive path of only letters, digits, space and _ \ . : ( ) + - (TH-30): safe inside the quoted .bat line.
function Test-LauncherPath([string]$Path) {
  return ($Path -match '^[A-Za-z]:\\[A-Za-z0-9 _\\.:()+-]+$') -and ($Path -notmatch '(^|\\)\.\.(\\|$)')
}

function Test-ReparsePoint([string]$Path) {
  $item = Get-Item -LiteralPath $Path -Force -ErrorAction SilentlyContinue
  return ($null -ne $item) -and (($item.Attributes -band [IO.FileAttributes]::ReparsePoint) -ne 0)
}

function Get-TargetArch {
  $arch = $env:PROCESSOR_ARCHITEW6432
  if (-not $arch) { $arch = $env:PROCESSOR_ARCHITECTURE }
  switch ($arch) {
    'AMD64' { return 'amd64' }
    'ARM64' { return 'arm64' }
    default { Fail "unsupported system: windows/$arch (only amd64 and arm64 are built)" }
  }
}

function Find-RetroBat {
  if ($RetroBatPath) {
    if ((Test-Path -LiteralPath (Join-Path $RetroBatPath 'RetroBat.exe')) -and
        (Test-Path -LiteralPath (Join-Path $RetroBatPath 'retrobat.ini'))) { return $RetroBatPath.TrimEnd('\') }
    Fail "$RetroBatPath does not contain RetroBat.exe and retrobat.ini"
  }
  if (Test-Path -LiteralPath 'C:\RetroBat\RetroBat.exe') { return 'C:\RetroBat' }
  return ''
}

# Explicit ACL for a folder outside %LocalAppData% (TH-31): no inherited write for other users.
function Protect-Folder([string]$Dir) {
  $me = [Security.Principal.WindowsIdentity]::GetCurrent().User.Value
  & icacls.exe $Dir /inheritance:r /grant:r "*${me}:(OI)(CI)F" '*S-1-5-18:(OI)(CI)F' '*S-1-5-32-544:(OI)(CI)F' '*S-1-5-32-545:(OI)(CI)RX' | Out-Null
  if ($LASTEXITCODE -ne 0) { Fail "could not set the ACL on $Dir; nothing was installed" }
}

function Get-ExpectedHash([string]$ChecksumsFile, [string]$Name) {
  $found = @()
  foreach ($line in Get-Content -LiteralPath $ChecksumsFile) {
    if ($line -match '^([0-9a-fA-F]{64})\s+\*?(\S+)\s*$' -and $Matches[2] -ceq $Name) { $found += $Matches[1].ToLowerInvariant() }
  }
  if ($found.Count -ne 1) { Fail "checksums.txt has no single valid SHA-256 line for $Name" }
  return $found[0]
}

# Runs a native tool and returns its exit code and combined output. Windows PowerShell 5.1 turns native stderr into
# a terminating error under $ErrorActionPreference = 'Stop', so the preference is relaxed only around the call.
function Invoke-Native([string]$Exe, [string[]]$Arguments) {
  $previous = $ErrorActionPreference
  $ErrorActionPreference = 'Continue'
  try {
    $text = (& $Exe @Arguments 2>&1 | ForEach-Object { "$_" }) -join "`n"
    return [pscustomobject]@{ Code = $LASTEXITCODE; Output = $text }
  } finally { $ErrorActionPreference = $previous }
}

function Test-Signature([string]$Url, [string]$Tag, [string]$Dir, [string]$ZipPath) {
  if (Get-Command cosign -ErrorAction SilentlyContinue) {
    $bundle = Join-Path $Dir 'checksums.txt.sigstore.json'
    try { Get-Remote "$Url/checksums.txt.sigstore.json" $bundle } catch { Fail 'cosign is installed but the signature bundle could not be downloaded' }
    $r = Invoke-Native 'cosign' @('verify-blob', '--bundle', $bundle, '--certificate-identity', "$SignerIdentity$Tag", '--certificate-oidc-issuer', $OidcIssuer, (Join-Path $Dir 'checksums.txt'))
    if ($r.Code -ne 0) { Fail "cosign signature verification failed: $($r.Output)" }
    return 'cosign signature verified'
  }
  if (Get-Command gh -ErrorAction SilentlyContinue) {
    if ((Invoke-Native 'gh' @('auth', 'status')).Code -eq 0) {
      $r = Invoke-Native 'gh' @('attestation', 'verify', $ZipPath, '--repo', $Repo, '--signer-workflow', "$Repo/.github/workflows/release.yml", '--source-ref', "refs/tags/$Tag")
      if ($r.Code -ne 0) { Fail "gh attestation verification failed: $($r.Output)" }
      return 'GitHub attestation verified'
    }
  }
  return 'signature not checked (cosign or a logged-in gh not found); SHA-256 only'
}

# Reads only the entry named exactly mothdex.exe (no path, so nothing can be extracted elsewhere).
function Expand-Binary([string]$Zip, [string]$Dest) {
  Add-Type -AssemblyName System.IO.Compression.FileSystem
  $archive = [IO.Compression.ZipFile]::OpenRead($Zip)
  try {
    $entry = $archive.Entries | Where-Object { $_.FullName -ceq 'mothdex.exe' } | Select-Object -First 1
    if (-not $entry) { Fail 'the archive does not contain mothdex.exe at its root' }
    $in = $entry.Open()
    try {
      $out = [IO.File]::Open($Dest, [IO.FileMode]::Create, [IO.FileAccess]::Write)
      try { $in.CopyTo($out) } finally { $out.Dispose() }
    } finally { $in.Dispose() }
  } finally { $archive.Dispose() }
}

function Test-HasMarker([string]$Path) {
  if (Test-ReparsePoint $Path) { return $false }
  foreach ($line in Get-Content -LiteralPath $Path -ErrorAction SilentlyContinue) { if ($line -ceq $Marker) { return $true } }
  return $false
}

# Fixed template; only the validated absolute path varies. Atomic; never overwrites a file without the marker.
function Install-Launcher([string]$LauncherDir, [string]$Exe) {
  $target = Join-Path $LauncherDir $LauncherName
  if ((Test-Path -LiteralPath $target) -and -not (Test-HasMarker $target)) {
    Write-Warning "keeping existing ${target}: it was not created by this installer"; return
  }
  if (-not (Test-LauncherPath $Exe)) {
    Write-Warning "the install path '$Exe' has characters not allowed in a launcher: launcher skipped (use -BinDir with a plain path)"; return
  }
  if (-not (Test-Path -LiteralPath $LauncherDir)) { New-Item -ItemType Directory -Path $LauncherDir | Out-Null }
  $text = "@echo off`r`n$Marker`r`nsetlocal DisableDelayedExpansion`r`n`"$Exe`" tui %*`r`n"
  $tmp = Join-Path $LauncherDir ".$LauncherName.$PID.tmp"
  [IO.File]::WriteAllText($tmp, $text, (New-Object Text.UTF8Encoding($false)))
  Move-Item -LiteralPath $tmp -Destination $target -Force
  Write-Output "Launcher: $target"
}

# --- main ----------------------------------------------------------------------------------------------------------

$arch = Get-TargetArch
$retrobat = Find-RetroBat
if (-not $BinDir) { $BinDir = Join-Path $env:LOCALAPPDATA 'Programs\mothdex' }
$BinDir = $BinDir.TrimEnd('\')
$exe = Join-Path $BinDir 'mothdex.exe'
$launcherDir = ''
if ($retrobat -and -not $NoLauncher) { $launcherDir = Join-Path $retrobat 'roms\windows' }
$profileId = 'generic'
if ($retrobat) { $profileId = 'retrobat' }
Write-Output "System: windows_$arch, profile: $profileId"

if ($Uninstall) {
  if (Test-Path -LiteralPath $exe) {
    if ($DryRun) { Write-Output "Would remove $exe" } else { Remove-Item -LiteralPath $exe -Force; Write-Output "Removed $exe" }
  } else { Write-Output "No binary at $exe" }
  if ($launcherDir) {
    $launcher = Join-Path $launcherDir $LauncherName
    if (Test-Path -LiteralPath $launcher) {
      if (Test-HasMarker $launcher) {
        if ($DryRun) { Write-Output "Would remove $launcher" } else { Remove-Item -LiteralPath $launcher -Force; Write-Output "Removed $launcher" }
      } else { Write-Warning "keeping $launcher`: it was not created by this installer" }
    }
  }
  Write-Output 'Your configuration, cache and ROMs were not touched.'
  return
}

$work = Join-Path ([IO.Path]::GetTempPath()) ("mothdex-install-" + [Guid]::NewGuid().ToString('N'))
New-Item -ItemType Directory -Path $work | Out-Null
try {
  if (-not $Version) {
    Get-Remote "$Base/latest/download/checksums.txt" (Join-Path $work 'latest.txt')
    $names = @(Get-Content -LiteralPath (Join-Path $work 'latest.txt') | ForEach-Object {
        if ($_ -match "^[0-9a-fA-F]{64}\s+\*?mothdex_(.+)_windows_$arch\.zip\s*$") { $Matches[1] } })
    if ($names.Count -ne 1) { Fail "the latest release has no single archive for windows_$arch" }
    $Version = $names[0]
  }
  $Version = $Version -replace '^v', ''
  if (-not (Test-Version $Version)) { Fail "invalid version '$Version' (expected X.Y.Z or X.Y.Z-rc.1)" }
  $tag = "v$Version"
  $url = "$Base/download/$tag"
  $zipName = "mothdex_${Version}_windows_$arch.zip"
  $zip = Join-Path $work $zipName

  Write-Output "Downloading $zipName from $url"
  Get-Remote "$url/checksums.txt" (Join-Path $work 'checksums.txt')
  Get-Remote "$url/$zipName" $zip

  $signature = Test-Signature $url $tag $work $zip
  $want = Get-ExpectedHash (Join-Path $work 'checksums.txt') $zipName
  $have = (Get-FileHash -LiteralPath $zip -Algorithm SHA256).Hash.ToLowerInvariant()
  if ($have -ne $want) { Fail "SHA-256 mismatch for ${zipName}: nothing was installed" }

  $staged = Join-Path $work 'mothdex.exe'
  Expand-Binary $zip $staged

  if ($DryRun) {
    Write-Output "Dry run: nothing was installed. Verified $zipName ($signature)."
    Write-Output "Would install: $exe"
    if ($launcherDir) { Write-Output "Would write: $(Join-Path $launcherDir $LauncherName)" }
    return
  }

  if (-not (Test-Path -LiteralPath $BinDir)) { New-Item -ItemType Directory -Path $BinDir | Out-Null }
  if (-not $BinDir.StartsWith($env:LOCALAPPDATA, [StringComparison]::OrdinalIgnoreCase)) { Protect-Folder $BinDir }
  $new = Join-Path $BinDir ".mothdex.new.$PID.exe"
  Copy-Item -LiteralPath $staged -Destination $new -Force
  Move-Item -LiteralPath $new -Destination $exe -Force
  Write-Output "Installed $exe $Version ($signature)"
  if ($launcherDir) { Install-Launcher $launcherDir $exe }
  & $exe --version
  Write-Output "Add $BinDir to your PATH to run 'mothdex' directly."
} finally {
  Remove-Item -LiteralPath $work -Recurse -Force -ErrorAction SilentlyContinue
}
