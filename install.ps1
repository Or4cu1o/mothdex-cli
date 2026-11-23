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
  When the release lists a mothdex-gui_ archive for this machine it also installs mothdex-gui.exe and SDL2.dll, after
  proving that the GUI starts (`mothdex-gui.exe --probe`, run from the install folder with a minimal environment), and
  the licence notices in mothdex-licenses. Default location: %LocalAppData%\Programs\mothdex (per-user, not writable by
  other users). With RetroBat found and the GUI installed, a launcher (Mothdex.bat, a Port that runs mothdex-gui and nothing
  else) is written to <RetroBat>\roms\windows, but only when that folder is not writable by other users. See docs/contracts/cli/ports.md and
  docs/contracts/cli/release-artifacts.md.

.PARAMETER Version
  Release to install (X.Y.Z or vX.Y.Z; pre-releases only by naming them). Default: the latest non-pre-release.
.PARAMETER RetroBatPath
  RetroBat folder (contains RetroBat.exe and retrobat.ini). Default: C:\RetroBat when RetroBat.exe is there.
.PARAMETER BinDir
  Install here instead of %LocalAppData%\Programs\mothdex. A folder outside %LocalAppData% gets an explicit ACL
  (current user, SYSTEM and Administrators full control; Users read and execute).
.PARAMETER Launcher
  Create the RetroBat launcher (the default). It is written only when the GUI is installed: a Port has no terminal to fall back to
  the text interface, so without the GUI no launcher is written and an existing one of ours is removed.
.PARAMETER NoLauncher
  Do not create or rewrite the launcher.
.PARAMETER Gui
  Install mothdex-gui when the release has one (the default).
.PARAMETER NoGui
  Install the CLI only; an installed mothdex-gui.exe and SDL2.dll are removed (upgrade).
.PARAMETER DryRun
  Download and verify, print what would be installed, leave nothing changed (the GUI check runs in an existing install folder and removes its staging file).
.PARAMETER Uninstall
  Remove the binaries, the licence notices and the launcher created by this installer.
#>
[CmdletBinding()]
param(
  [string]$Version = '',
  [string]$RetroBatPath = '',
  [string]$BinDir = '',
  [switch]$Launcher,
  [switch]$NoLauncher,
  [switch]$Gui,
  [switch]$NoGui,
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
$MarkerV1 = 'rem mothdex-launcher v1'
$MarkerV2 = 'rem mothdex-launcher v2'
$Marker = 'rem mothdex-launcher v3'
$LauncherName = 'Mothdex.bat'
$LicensesName = 'mothdex-licenses'
# The notices this installer ships (T-70 IC-5): the only names ever removed from mothdex-licenses.
$NoticeNames = @('README.md', 'Handjet-OFL-1.1.txt', 'NotoSans-OFL-1.1.txt', 'SDL_GameControllerDB-zlib.txt', 'SDL2-zlib.txt', 'purego-Apache-2.0.txt', 'x-image-BSD-3-Clause.txt', 'go-modules-mothdex.txt', 'go-modules-mothdex-gui.txt')
$ProbeSeconds = 20
$RefreshUrl = 'http://127.0.0.1:1234/reloadgames'
$script:Failed = $false

function Fail([string]$Message) { throw "mothdex install: $Message" }

if ($Gui -and $NoGui) { Fail '-Gui and -NoGui cannot be used together' }
if ($Launcher -and $NoLauncher) { Fail '-Launcher and -NoLauncher cannot be used together' }

# Only URLs built from $Base are ever downloaded.
function Get-Remote([string]$Url, [string]$Dest) {
  if (-not $Url.StartsWith("$Base/", [StringComparison]::Ordinal)) { Fail "refusing to download $Url" }
  try {
    [Net.ServicePointManager]::SecurityProtocol = [Net.ServicePointManager]::SecurityProtocol -bor [Net.SecurityProtocolType]::Tls12
  } catch { }
  $response = Invoke-WebRequest -Uri $Url -OutFile $Dest -UseBasicParsing -MaximumRedirection 5 -PassThru
  # Windows PowerShell 5.1 can follow an https to http redirect: check where the file really came from.
  $final = $null
  $inner = $response.BaseResponse
  if ($inner) {
    if ($inner.PSObject.Properties['ResponseUri']) { $final = $inner.ResponseUri }
    elseif ($inner.PSObject.Properties['RequestMessage'] -and $inner.RequestMessage) { $final = $inner.RequestMessage.RequestUri }
  }
  # Fail closed: an unreadable final address is refused like a non-https one.
  if ((-not $final) -or ($final.Scheme -ne 'https')) {
    Remove-Item -LiteralPath $Dest -Force -ErrorAction SilentlyContinue
    if ($final) { Fail "refusing $Url : it was redirected to $final, which is not https" }
    Fail "refusing $Url : the final address of the download could not be read"
  }
}

function Test-Version([string]$Value) { return $Value -match '^[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z][0-9A-Za-z.-]*)?$' }

# A file name taken from an archive (T-70 IC-4, IC-5): plain characters only, never '.' or '..'.
function Test-SafeName([string]$Name) {
  return ($Name -cmatch '^[A-Za-z0-9._-]+\z') -and ($Name -ne '.') -and ($Name -ne '..')
}

# Absolute drive path of only letters, digits, space and _ \ . : ( ) + - (TH-30): safe inside the quoted .bat line.
function Test-LauncherPath([string]$Path) {
  return ($Path -match '^[A-Za-z]:\\[A-Za-z0-9 _\\.:()+-]+$') -and ($Path -notmatch '(^|\\)\.\.(\\|$)')
}

function Test-ReparsePoint([string]$Path) {
  $item = Get-Item -LiteralPath $Path -Force -ErrorAction SilentlyContinue
  return ($null -ne $item) -and (($item.Attributes -band [IO.FileAttributes]::ReparsePoint) -ne 0)
}

# BinDir must be an absolute drive path without '..' segments; returns the normalized full path (TH-31).
function Resolve-BinDir([string]$Path) {
  if (($Path -notmatch '^[A-Za-z]:\\') -or ($Path -match '(^|[\\/])\.\.([\\/]|$)') -or ($Path.IndexOfAny([IO.Path]::GetInvalidPathChars()) -ge 0)) {
    Fail "BinDir '$Path' is not allowed: use an absolute drive path without '..'"
  }
  return [IO.Path]::GetFullPath($Path).TrimEnd('\')
}

function Test-UnderPath([string]$Path, [string]$Parent) {
  return ($Path.TrimEnd('\') + '\').StartsWith($Parent.TrimEnd('\') + '\', [StringComparison]::OrdinalIgnoreCase)
}

# An existing folder outside %LocalAppData% is used only if its owner is this user, Administrators, SYSTEM or
# TrustedInstaller and no other principal can write to it (TH-31). It is verified, never rewritten. The same rule
# decides whether the launcher folder can be used (T-70 IC-7).
function Test-FolderTrusted([string]$Dir) {
  $me = [Security.Principal.WindowsIdentity]::GetCurrent().User.Value
  $trusted = @($me, 'S-1-5-18', 'S-1-5-32-544', 'S-1-3-0', 'S-1-5-80-956008885-3418522649-1831038044-1853292631-2271478464')
  $acl = Get-Acl -LiteralPath $Dir
  if ($trusted -notcontains $acl.GetOwner([Security.Principal.SecurityIdentifier]).Value) { return $false }
  # WriteData, AppendData, DeleteSubdirectoriesAndFiles, Delete, ChangePermissions, TakeOwnership, GenericAll, GenericWrite
  $writeMask = 0x2 -bor 0x4 -bor 0x40 -bor 0x10000 -bor 0x40000 -bor 0x80000 -bor 0x10000000 -bor 0x40000000
  foreach ($rule in $acl.GetAccessRules($true, $true, [Security.Principal.SecurityIdentifier])) {
    if ($rule.AccessControlType -ne 'Allow') { continue }
    if ($trusted -contains $rule.IdentityReference.Value) { continue }
    if (([int]$rule.FileSystemRights -band $writeMask) -ne 0) { return $false }
  }
  return $true
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

# How many lines of the verified checksums.txt name the archive (T-70 IC-3).
function Get-ListedCount([string]$ChecksumsFile, [string]$Name) {
  $count = 0
  foreach ($line in Get-Content -LiteralPath $ChecksumsFile) {
    if ($line -match '^[0-9a-fA-F]{64}\s+\*?(\S+)\s*$' -and $Matches[1] -ceq $Name) { $count++ }
  }
  return $count
}

function Assert-ArchiveHash([string]$File, [string]$ChecksumsFile, [string]$Name) {
  $want = Get-ExpectedHash $ChecksumsFile $Name
  $have = (Get-FileHash -LiteralPath $File -Algorithm SHA256).Hash.ToLowerInvariant()
  if ($have -ne $want) { Fail "SHA-256 mismatch for ${Name}: nothing was installed" }
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

# T-70 IC-4: only members with a known name are accepted, each is written by exact name (never ExtractToDirectory)
# and must be a regular file. Returns the names of the THIRD_PARTY notices.
function Expand-GuiArchive([string]$Zip, [string]$ZipName, [string]$Dest) {
  Add-Type -AssemblyName System.IO.Compression.FileSystem
  $archive = [IO.Compression.ZipFile]::OpenRead($Zip)
  try {
    $wanted = New-Object System.Collections.ArrayList
    $haveExe = $false
    $haveDll = $false
    foreach ($entry in $archive.Entries) {
      $name = $entry.FullName
      if ($name -ceq 'mothdex-gui.exe') { $haveExe = $true; [void]$wanted.Add(@($entry, 'mothdex-gui.exe')); continue }
      if ($name -ceq 'SDL2.dll') { $haveDll = $true; [void]$wanted.Add(@($entry, 'SDL2.dll')); continue }
      if (@('LICENSE', 'README.md', 'CHANGELOG.md', 'THIRD_PARTY/') -ccontains $name) { continue }
      if ($name.StartsWith('THIRD_PARTY/', [StringComparison]::Ordinal)) {
        $base = $name.Substring(12)
        if (-not (Test-SafeName $base)) { Fail "$ZipName has an unsafe member name: nothing was installed" }
        if ($NoticeNames -cnotcontains $base) { Fail "$ZipName has a THIRD_PARTY member this installer does not know ($base): nothing was installed. This installer may be older than the release: fetch install.ps1 from the release tag (raw.githubusercontent.com/$Repo/<tag>/install.ps1)" }
        [void]$wanted.Add(@($entry, ('THIRD_PARTY\' + $base))); continue
      }
      Fail "$ZipName has an unexpected member: nothing was installed"
    }
    if (-not $haveExe) { Fail "$ZipName does not contain mothdex-gui.exe" }
    if (-not $haveDll) { Fail "$ZipName does not contain SDL2.dll" }
    $notices = @()
    foreach ($pair in $wanted) {
      $entry = $pair[0]
      $relative = $pair[1]
      # a link stored in a zip carries a Unix file type in the high bits of the external attributes
      if ((([int]$entry.ExternalAttributes -shr 16) -band 0xF000) -eq 0xA000) { Fail "$ZipName`: $relative is not a regular file" }
      $target = Join-Path $Dest $relative
      $parent = Split-Path -Parent $target
      if (-not (Test-Path -LiteralPath $parent)) { New-Item -ItemType Directory -Path $parent | Out-Null }
      $in = $entry.Open()
      try {
        $out = [IO.File]::Open($target, [IO.FileMode]::Create, [IO.FileAccess]::Write)
        try { $in.CopyTo($out) } finally { $out.Dispose() }
      } finally { $in.Dispose() }
      $item = Get-Item -LiteralPath $target -Force
      if ($item.PSIsContainer -or (($item.Attributes -band [IO.FileAttributes]::ReparsePoint) -ne 0)) { Fail "$ZipName`: $relative is not a regular file" }
      if ($relative.StartsWith('THIRD_PARTY\')) { $notices += $relative.Substring(12) }
    }
    return $notices
  } finally { $archive.Dispose() }
}

# T-70 IC-1, IC-10: the staged mothdex-gui.exe is run with an environment of only SystemRoot, PATH (System32) and
# TEMP, and a time bound. Exit 0 is eligible; a timeout is not eligible; anything else is a probe failure.
function Invoke-GuiProbe([string]$Exe) {
  $psi = New-Object Diagnostics.ProcessStartInfo
  $psi.FileName = $Exe
  $psi.Arguments = '--probe'
  $psi.UseShellExecute = $false
  $psi.RedirectStandardOutput = $true
  $psi.RedirectStandardError = $true
  $psi.CreateNoWindow = $true
  $psi.WorkingDirectory = [IO.Path]::GetDirectoryName($Exe)
  $psi.EnvironmentVariables.Clear()
  $psi.EnvironmentVariables['SystemRoot'] = $env:SystemRoot
  $psi.EnvironmentVariables['PATH'] = Join-Path $env:SystemRoot 'System32'
  $psi.EnvironmentVariables['TEMP'] = [IO.Path]::GetTempPath().TrimEnd('\')
  try {
    $process = [Diagnostics.Process]::Start($psi)
  } catch {
    return [pscustomobject]@{ Verdict = 'error'; Code = -1; Message = 'the program could not be started' }
  }
  try {
    $stdout = $process.StandardOutput.ReadToEndAsync()
    $stderr = $process.StandardError.ReadToEndAsync()
    if (-not $process.WaitForExit($ProbeSeconds * 1000)) {
      try { $process.Kill() } catch { }
      [void]$process.WaitForExit(5000)   # the staged exe stays locked until the process is really gone
      return [pscustomobject]@{ Verdict = 'not-eligible'; Code = 124; Message = "the probe did not finish within $ProbeSeconds seconds" }
    }
    $process.WaitForExit()
    $text = ($stdout.Result + "`n" + $stderr.Result).Trim()
    $first = ($text -split "`r?`n")[0]
    $first = $first -replace '[\x00-\x1f\x7f]', ''
    if ($first.Length -gt 200) { $first = $first.Substring(0, 200) }
    if ($process.ExitCode -eq 0) { return [pscustomobject]@{ Verdict = 'eligible'; Code = 0; Message = $first } }
    return [pscustomobject]@{ Verdict = 'error'; Code = $process.ExitCode; Message = $first }
  } finally { $process.Dispose() }
}

# T-70 IC-2: the GUI and its SDL2.dll are staged in a folder inside BinDir (already proven trusted), probed there and
# moved into place on success. The staging folder is always removed.
function Install-Gui([string]$GuiStage, [string]$BinDirPath, [bool]$Move) {
  $stage = Join-Path $BinDirPath ".mothdex-gui.new.$PID"
  New-Item -ItemType Directory -Path $stage | Out-Null
  try {
    Copy-Item -LiteralPath (Join-Path $GuiStage 'mothdex-gui.exe') -Destination (Join-Path $stage 'mothdex-gui.exe')
    Copy-Item -LiteralPath (Join-Path $GuiStage 'SDL2.dll') -Destination (Join-Path $stage 'SDL2.dll')
    $result = Invoke-GuiProbe (Join-Path $stage 'mothdex-gui.exe')
    if ($result.Verdict -eq 'eligible' -and $Move) {
      Move-Item -LiteralPath (Join-Path $stage 'SDL2.dll') -Destination (Join-Path $BinDirPath 'SDL2.dll') -Force
      Move-Item -LiteralPath (Join-Path $stage 'mothdex-gui.exe') -Destination (Join-Path $BinDirPath 'mothdex-gui.exe') -Force
    }
    return $result
  } finally {
    # a few tries: antivirus or a process that has just been killed can hold the files for a moment
    for ($try = 0; $try -lt 10; $try++) {
      foreach ($name in 'mothdex-gui.exe', 'SDL2.dll') { Remove-Item -LiteralPath (Join-Path $stage $name) -Force -ErrorAction SilentlyContinue }
      try { [IO.Directory]::Delete($stage, $false); break } catch { Start-Sleep -Milliseconds 300 }
    }
    if (Test-Path -LiteralPath $stage) { Write-Warning "could not remove the staging folder $stage" }
  }
}

# T-70 IC-5: removes the regular files named like a notice this installer ships from a licences folder, then the folder itself
# if it is empty: no recursive delete, no link followed, anything else is kept.
function Remove-Licenses([string]$Dir, [bool]$Quiet) {
  if (Test-ReparsePoint $Dir) { if (-not $Quiet) { Write-Warning "not touching ${Dir}: it is a link" }; return }
  if (-not (Test-Path -LiteralPath $Dir -PathType Container)) { return }
  foreach ($item in @(Get-ChildItem -LiteralPath $Dir -Force)) {
    if ((-not $item.PSIsContainer) -and (($item.Attributes -band [IO.FileAttributes]::ReparsePoint) -eq 0) -and ($NoticeNames -ccontains $item.Name)) {
      $item.Attributes = [IO.FileAttributes]::Normal
      Remove-Item -LiteralPath $item.FullName -Force
    } elseif (-not $Quiet) { Write-Warning "keeping $($item.FullName): this installer did not write it" }
  }
  try { [IO.Directory]::Delete($Dir, $false) } catch { if (-not $Quiet) { Write-Warning "kept ${Dir}: it holds files this installer did not write" } }
}

# Temp folder plus rename; read-only files; never through a link (T-70 IC-5).
function Install-Licenses([string]$GuiStage, [string[]]$Names, [string]$BinDirPath) {
  if ($Names.Count -eq 0) { Write-Warning 'the GUI archive carries no THIRD_PARTY notices'; return }
  $dest = Join-Path $BinDirPath $LicensesName
  if ((Test-ReparsePoint $dest) -or ((Test-Path -LiteralPath $dest) -and -not (Test-Path -LiteralPath $dest -PathType Container))) {
    Write-Warning "refusing ${dest}: it is a link or not a folder; licence notices not installed"; return
  }
  $tmp = Join-Path $BinDirPath ".$LicensesName.new.$PID"
  New-Item -ItemType Directory -Path $tmp | Out-Null
  try {
    foreach ($name in $Names) {
      $file = Join-Path $tmp $name
      Copy-Item -LiteralPath (Join-Path $GuiStage "THIRD_PARTY\$name") -Destination $file
      (Get-Item -LiteralPath $file).IsReadOnly = $true
    }
    if (Test-Path -LiteralPath $dest) { Remove-Licenses $dest $true }
    if (Test-Path -LiteralPath $dest) { Write-Warning "kept ${dest}: it holds files this installer did not write; licence notices not updated"; return }
    Move-Item -LiteralPath $tmp -Destination $dest
  } finally {
    if (Test-Path -LiteralPath $tmp) { Remove-Licenses $tmp $true }
  }
}

# Whenever this release's GUI is not installed (-NoGui, not listed, not eligible, probe error) the GUI files of an earlier
# install go too: the launcher must not start an older GUI next to this CLI, or one the probe just showed cannot start.
# SDL2.dll is ours only next to our mothdex-gui.exe.
function Remove-GuiFiles([string]$BinDirPath, [string]$Why, [bool]$Dry) {
  $gui = Join-Path $BinDirPath 'mothdex-gui.exe'
  if (Test-Path -LiteralPath $gui) {
    if ($Dry) { Write-Output "Would remove $gui and SDL2.dll ($Why)" } else {
      Remove-Item -LiteralPath $gui -Force
      $dll = Join-Path $BinDirPath 'SDL2.dll'
      if (Test-Path -LiteralPath $dll) { Remove-Item -LiteralPath $dll -Force }
      Write-Output "Removed $gui and SDL2.dll ($Why)"
    }
  }
  $notices = Join-Path $BinDirPath $LicensesName
  if ((Test-Path -LiteralPath $notices) -or (Test-ReparsePoint $notices)) {
    if ($Dry) { Write-Output "Would remove $notices ($Why)" } else { Remove-Licenses $notices $false }
  }
}

function Test-HasMarker([string]$Path) {
  if (Test-ReparsePoint $Path) { return $false }
  foreach ($line in Get-Content -LiteralPath $Path -ErrorAction SilentlyContinue) { if (($line -ceq $Marker) -or ($line -ceq $MarkerV2) -or ($line -ceq $MarkerV1)) { return $true } }
  return $false
}

# Fixed template (marker v2); only the validated absolute path varies. No argument forwarding (T-70 IC-6). Atomic; never
# overwrites a file without a marker; written only into a folder other users cannot write to (T-70 IC-7).
function Install-Launcher([string]$LauncherDir, [string]$BinDirPath) {
  $target = Join-Path $LauncherDir $LauncherName
  if ((Test-Path -LiteralPath $target) -and -not (Test-HasMarker $target)) {
    Write-Warning "keeping existing ${target}: it was not created by this installer"; return
  }
  if (-not (Test-LauncherPath $BinDirPath)) {
    Write-Warning "the install path '$BinDirPath' has characters not allowed in a launcher: launcher skipped (use -BinDir with a plain path)"; return
  }
  if (-not (Test-Path -LiteralPath $LauncherDir)) { New-Item -ItemType Directory -Path $LauncherDir | Out-Null }
  if ((Test-ReparsePoint $LauncherDir) -or (-not (Test-FolderTrusted $LauncherDir))) {
    Write-Warning "$LauncherDir is owned by another user, is a link, or can be written by other users: launcher skipped (restrict its permissions to enable it)"; return
  }
  $gui = Join-Path $BinDirPath 'mothdex-gui.exe'
  $text = "@echo off`r`n$Marker`r`nsetlocal DisableDelayedExpansion`r`n`"$gui`"`r`nexit /b %errorlevel%`r`n"
  $tmp = Join-Path $LauncherDir ".$LauncherName.$PID.tmp"
  [IO.File]::WriteAllText($tmp, $text, (New-Object Text.UTF8Encoding($false)))
  Move-Item -LiteralPath $tmp -Destination $target -Force
  $script:LauncherWritten = $true
  Write-Output "Launcher: $target (opt out with -NoLauncher; -Uninstall removes it)"
}

# D32 (PRT-G8): without the GUI a launcher of ours would start nothing, so it is removed; never a file without one of our
# markers (Test-HasMarker refuses a reparse point too).
function Remove-Launcher([string]$LauncherDir, [string]$Why, [bool]$Dry) {
  if (-not $LauncherDir) { return }
  $target = Join-Path $LauncherDir $LauncherName
  if (-not (Test-Path -LiteralPath $target)) { return }
  if (-not (Test-HasMarker $target)) { return }
  if ($Dry) { Write-Output "Would remove $target ($Why)"; return }
  Remove-Item -LiteralPath $target -Force
  $script:LauncherRemoved = $true
  Write-Output "Removed $target ($Why)"
}

# T-70 IC-11 (TH-22): ask RetroBat to reload its game list. Literal loopback URL, no proxy, no redirect, 3 seconds,
# never fatal.
function Invoke-FrontendRefresh {
  $request = @{ Uri = $RefreshUrl; UseBasicParsing = $true; MaximumRedirection = 0; TimeoutSec = 3 }
  $savedProxy = $null
  $proxyChanged = $false
  if ((Get-Command Invoke-WebRequest).Parameters.ContainsKey('NoProxy')) {
    $request['NoProxy'] = $true
  } else {
    $savedProxy = [Net.WebRequest]::DefaultWebProxy
    [Net.WebRequest]::DefaultWebProxy = $null
    $proxyChanged = $true
  }
  try {
    [void](Invoke-WebRequest @request)
    Write-Output 'Asked RetroBat to reload its game list.'
  } catch {
    Write-Output 'note: could not ask RetroBat to reload its game list; the Port shows after RetroBat reloads or restarts.'
  } finally {
    if ($proxyChanged) { [Net.WebRequest]::DefaultWebProxy = $savedProxy }
  }
}

# --- main ----------------------------------------------------------------------------------------------------------

$script:LauncherWritten = $false
$script:LauncherRemoved = $false
$arch = Get-TargetArch
$retrobat = Find-RetroBat
if (-not $BinDir) { $BinDir = Join-Path $env:LOCALAPPDATA 'Programs\mothdex' }
$BinDir = Resolve-BinDir $BinDir
$binInLocalAppData = Test-UnderPath $BinDir ([IO.Path]::GetFullPath($env:LOCALAPPDATA))
if (Test-ReparsePoint $BinDir) { Fail "refusing BinDir '$BinDir': it is a junction or symbolic link" }
if ((-not $binInLocalAppData) -and (Test-Path -LiteralPath $BinDir) -and (-not (Test-FolderTrusted $BinDir))) {
  Fail "refusing BinDir '$BinDir': it exists, and its owner is not you or an administrator, or another user can write to it"
}
$exe = Join-Path $BinDir 'mothdex.exe'
$guiExe = Join-Path $BinDir 'mothdex-gui.exe'
$launcherDir = ''
if ($retrobat) { $launcherDir = Join-Path $retrobat 'roms\windows' }
$profileId = 'generic'
if ($retrobat) { $profileId = 'retrobat' }
Write-Output "System: windows_$arch, profile: $profileId"

if ($Uninstall) {
  if (Test-Path -LiteralPath $exe) {
    if ($DryRun) { Write-Output "Would remove $exe" } else { Remove-Item -LiteralPath $exe -Force; Write-Output "Removed $exe" }
  } else { Write-Output "No binary at $exe" }
  if (Test-Path -LiteralPath $guiExe) {
    if ($DryRun) { Write-Output "Would remove $guiExe" } else { Remove-Item -LiteralPath $guiExe -Force; Write-Output "Removed $guiExe" }
    # SDL2.dll is ours only next to our mothdex-gui.exe
    $dll = Join-Path $BinDir 'SDL2.dll'
    if (Test-Path -LiteralPath $dll) {
      if ($DryRun) { Write-Output "Would remove $dll" } else { Remove-Item -LiteralPath $dll -Force; Write-Output "Removed $dll" }
    }
  }
  $licenses = Join-Path $BinDir $LicensesName
  if ((Test-Path -LiteralPath $licenses) -or (Test-ReparsePoint $licenses)) {
    if ($DryRun) { Write-Output "Would remove $licenses" } else {
      Remove-Licenses $licenses $false
      if (-not (Test-Path -LiteralPath $licenses)) { Write-Output "Removed $licenses" }
    }
  }
  if ($launcherDir) {
    $launcherFile = Join-Path $launcherDir $LauncherName
    if (Test-Path -LiteralPath $launcherFile) {
      if (Test-HasMarker $launcherFile) {
        if ($DryRun) { Write-Output "Would remove $launcherFile" } else { Remove-Item -LiteralPath $launcherFile -Force; Write-Output "Removed $launcherFile" }
      } else { Write-Warning "keeping $launcherFile`: it was not created by this installer" }
    }
  }
  Write-Output 'Your configuration, data, history, cache and ROMs were not touched.'
  return
}

$work = Join-Path ([IO.Path]::GetTempPath()) ("mothdex-install-" + [Guid]::NewGuid().ToString('N'))
$new = ''
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
  $checksums = Join-Path $work 'checksums.txt'

  Write-Output "Downloading $zipName from $url"
  Get-Remote "$url/checksums.txt" $checksums
  Get-Remote "$url/$zipName" $zip

  $signature = Test-Signature $url $tag $work $zip
  Assert-ArchiveHash $zip $checksums $zipName

  $staged = Join-Path $work 'mothdex.exe'
  Expand-Binary $zip $staged

  # T-70 IC-3: whether a GUI archive exists comes only from the verified checksums.txt. Not listed: skip with a note.
  # Listed but not downloadable, or failing verification: abort the whole install, before anything is written.
  $guiZipName = "mothdex-gui_${Version}_windows_$arch.zip"
  $guiReady = $false
  $guiNotices = @()
  $guiStage = Join-Path $work 'gui'
  $listedCount = Get-ListedCount $checksums $guiZipName
  if ($listedCount -gt 1) { Fail "checksums.txt lists $guiZipName more than once" }
  if ($listedCount -eq 1) {
    if (-not $NoGui) {
      $guiZip = Join-Path $work $guiZipName
      try { Get-Remote "$url/$guiZipName" $guiZip } catch { Fail "$guiZipName is listed in checksums.txt but could not be downloaded: nothing was installed" }
      Assert-ArchiveHash $guiZip $checksums $guiZipName
      if ($signature -eq 'GitHub attestation verified') {
        $r = Invoke-Native 'gh' @('attestation', 'verify', $guiZip, '--repo', $Repo, '--signer-workflow', "$Repo/.github/workflows/release.yml", '--source-ref', "refs/tags/$tag")
        if ($r.Code -ne 0) { Fail "gh attestation verification failed for ${guiZipName}: $($r.Output)" }
      }
      New-Item -ItemType Directory -Path $guiStage | Out-Null
      $guiNotices = @(Expand-GuiArchive $guiZip $guiZipName $guiStage)
      $guiReady = $true
    }
  } elseif (-not $NoGui) {
    Write-Output "note: release $tag has no GUI archive for windows_${arch}: CLI only"
  }

  if ($DryRun) {
    Write-Output "Dry run: nothing was installed. Verified $zipName ($signature)."
    Write-Output "Would install: $exe"
    $dryGui = 'none' # eligible, none (the GUI would not be installed) or unknown (eligibility not checked)
    if ($guiReady) {
      Write-Output "GUI archive: $guiZipName (verified)"
      if (Test-Path -LiteralPath $BinDir) {
        $result = Install-Gui $guiStage $BinDir $false
        if ($result.Verdict -eq 'eligible') { $dryGui = 'eligible'; Write-Output "GUI eligibility: eligible ($($result.Message)); would install $guiExe, SDL2.dll and $(Join-Path $BinDir $LicensesName)" }
        elseif ($result.Verdict -eq 'not-eligible') { Write-Output "GUI eligibility: not eligible: $($result.Message); the GUI would not be installed"; Remove-GuiFiles $BinDir 'not eligible' $true }
        else { Write-Output "GUI eligibility: the probe failed (exit $($result.Code)): $($result.Message)"; Remove-GuiFiles $BinDir 'probe error' $true }
      } else { $dryGui = 'unknown'; Write-Output "GUI eligibility: not checked ($BinDir does not exist yet)" }
    } else {
      $why = 'this release has no GUI archive'
      if ($NoGui) { $why = '-NoGui' }
      Remove-GuiFiles $BinDir $why $true
    }
    if ($dryGui -eq 'eligible') {
      if ((-not $NoLauncher) -and $launcherDir) { Write-Output "Would write: $(Join-Path $launcherDir $LauncherName)" }
    } elseif ($dryGui -eq 'unknown') {
      if ((-not $NoLauncher) -and $launcherDir) { Write-Output 'Launcher: would be written only if the GUI turns out to be eligible' }
    } else {
      Remove-Launcher $launcherDir 'the GUI would not be installed' $true
    }
    return
  }

  if (-not (Test-Path -LiteralPath $BinDir)) {
    New-Item -ItemType Directory -Path $BinDir | Out-Null
    if (-not $binInLocalAppData) { Protect-Folder $BinDir }
  }
  $new = Join-Path $BinDir ".mothdex.new.$PID.exe"
  Copy-Item -LiteralPath $staged -Destination $new -Force
  Move-Item -LiteralPath $new -Destination $exe -Force
  Write-Output "Installed $exe $Version ($signature)"

  $guiInstalled = $false
  if ($guiReady) {
    $result = Install-Gui $guiStage $BinDir $true
    if ($result.Verdict -eq 'eligible') {
      $guiInstalled = $true
      Write-Output "Installed $guiExe ($($result.Message))"
      Install-Licenses $guiStage $guiNotices $BinDir
    } elseif ($result.Verdict -eq 'not-eligible') {
      Write-Warning "mothdex-gui was not installed: $($result.Message). The CLI works; no launcher is written, because a Port has no terminal for 'mothdex tui'."
    } else {
      # a probe failure of the Windows archive is a packaging bug: reported as an error, the CLI install still completes (IC-9)
      Write-Warning "error: mothdex-gui.exe --probe failed (exit $($result.Code)): $($result.Message)"
      Write-Warning 'error: mothdex-gui.exe and SDL2.dll were not installed; the CLI install is complete.'
      $script:Failed = $true
    }
  }
  if (-not $guiInstalled) {
    $why = 'this release has no GUI archive'
    if ($NoGui) { $why = '-NoGui' } elseif ($guiReady) { $why = 'not installed: see above' }
    Remove-GuiFiles $BinDir $why $false
  }

  # D32 (PRT-G8): the launcher is written only when the GUI is installed (and -NoLauncher was not given); without the GUI an existing
  # launcher of ours is removed, and a user's file is never touched
  if ($guiInstalled) {
    if ((-not $NoLauncher) -and $launcherDir) { Install-Launcher $launcherDir $BinDir }
    elseif ((-not $NoLauncher) -and $Launcher) { Write-Output 'note: RetroBat was not found: launcher skipped' }
  } else {
    if ($Launcher) { Write-Output "note: no launcher: it is written only when the GUI is installed (a Port has no terminal for 'mothdex tui')" }
    Remove-Launcher $launcherDir 'the GUI is not installed: a Port needs it' $false
  }
  if (($script:LauncherWritten -or $script:LauncherRemoved) -and ($profileId -eq 'retrobat')) { Invoke-FrontendRefresh }
  $check = Invoke-Native $exe @('--version')
  if ($check.Code -eq 0) { Write-Output $check.Output } else { Write-Warning "$exe --version exited with $($check.Code); the file is installed." }
  Write-Output "Add $BinDir to your PATH to run 'mothdex' directly."
} finally {
  if ($new) { Remove-Item -LiteralPath $new -Force -ErrorAction SilentlyContinue }
  Remove-Item -LiteralPath $work -Recurse -Force -ErrorAction SilentlyContinue
}
# the exit status is ours: the last native command above (the installed binary's --version) must not leak into it
if ($script:Failed) { exit 1 }
$global:LASTEXITCODE = 0
