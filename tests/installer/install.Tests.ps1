<#
  Tests for install.ps1 on Windows (run by the `installer-windows` CI job; needs no network).
  Invoke-WebRequest is shadowed by a function that serves a local fake release and refuses any other URL, and the
  "binary" in the fake zips is a copy of whoami.exe. Covers launcher quoting (TH-30), ACL (TH-31), marker and atomic
  launcher (TH-32) and verification (TH-33).
#>
Set-StrictMode -Version 2.0
$ErrorActionPreference = 'Stop'
$installer = Join-Path (Split-Path -Parent (Split-Path -Parent $PSScriptRoot)) 'install.ps1'
$root = Join-Path ([IO.Path]::GetTempPath()) ("mothdex-test-" + [Guid]::NewGuid().ToString('N'))
$release = Join-Path $root 'release'
New-Item -ItemType Directory -Path $release | Out-Null
Add-Type -AssemblyName System.IO.Compression.FileSystem

$script:pass = 0
$script:fail = 0
$global:urls = New-Object System.Collections.ArrayList
$script:message = ''
$base = 'https://github.com/Or4cu1o/mothdex-cli/releases/'
$global:fakeBase = $base
$global:fakeRelease = $release

function Check([string]$What, [bool]$Ok) {
  if ($Ok) { $script:pass++ } else { $script:fail++; Write-Output "FAIL: $What [last error: $($script:message)]" }
}

# Network double: only release URLs of Or4cu1o/mothdex-cli, served from $release.
function Invoke-WebRequest {
  param([string]$Uri, [string]$OutFile, [switch]$UseBasicParsing, [int]$MaximumRedirection)
  [void]$global:urls.Add($Uri)
  if (-not $Uri.StartsWith($global:fakeBase, [StringComparison]::Ordinal)) { throw "BLOCKED URL $Uri" }
  $rel = $Uri.Substring($global:fakeBase.Length) -replace '^download/', '' -replace '^latest/download/', 'latest/'
  $src = Join-Path $global:fakeRelease ($rel -replace '/', '\')
  if (-not (Test-Path -LiteralPath $src)) { throw "404 $Uri" }
  Copy-Item -LiteralPath $src -Destination $OutFile
}

function New-Tag([string]$Tag, [bool]$BadChecksum) {
  $ver = $Tag.TrimStart('v')
  $dir = Join-Path $release $Tag
  New-Item -ItemType Directory -Path $dir -Force | Out-Null
  $lines = @()
  foreach ($arch in 'amd64', 'arm64') {
    $stage = Join-Path $root "stage-$Tag-$arch"
    New-Item -ItemType Directory -Path $stage -Force | Out-Null
    Copy-Item "$env:SystemRoot\System32\whoami.exe" (Join-Path $stage 'mothdex.exe')
    Set-Content -Path (Join-Path $stage 'LICENSE') -Value 'license'
    $name = "mothdex_${ver}_windows_$arch.zip"
    [IO.Compression.ZipFile]::CreateFromDirectory($stage, (Join-Path $dir $name))
    $hash = (Get-FileHash (Join-Path $dir $name) -Algorithm SHA256).Hash.ToLowerInvariant()
    if ($BadChecksum) { $hash = ('0' * 64) }
    $lines += "$hash  $name"
  }
  Set-Content -Path (Join-Path $dir 'checksums.txt') -Value $lines
  Set-Content -Path (Join-Path $dir 'checksums.txt.sigstore.json') -Value '{}'
}

New-Tag 'v1.2.3' $false
New-Tag 'v1.1.0' $true
New-Item -ItemType Directory -Path (Join-Path $release 'latest') | Out-Null
Copy-Item (Join-Path $release 'v1.2.3\*') (Join-Path $release 'latest')

$env:PROCESSOR_ARCHITECTURE = 'AMD64'
$env:PROCESSOR_ARCHITEW6432 = ''
$env:LOCALAPPDATA = Join-Path $root 'Local'
New-Item -ItemType Directory -Path $env:LOCALAPPDATA | Out-Null

function Invoke-Install([hashtable]$Params) {
  $script:threw = $false
  $script:message = ''
  $global:urls.Clear()
  try { & $installer @Params *> $null } catch { $script:threw = $true; $script:message = $_.Exception.Message }
}

function New-RetroBat([string]$Name) {
  $rb = Join-Path $root $Name
  New-Item -ItemType Directory -Path (Join-Path $rb 'roms\windows') -Force | Out-Null
  Set-Content -Path (Join-Path $rb 'RetroBat.exe') -Value 'x'
  Set-Content -Path (Join-Path $rb 'retrobat.ini') -Value '[x]'
  return $rb
}

# 1. default install, no RetroBat: per-user folder, no launcher
Invoke-Install @{ Version = 'v1.2.3'; RetroBatPath = '' }
$exe = Join-Path $env:LOCALAPPDATA 'Programs\mothdex\mothdex.exe'
Check 'default install succeeds' (-not $script:threw)
Check 'binary under LocalAppData' (Test-Path $exe)
Check 'only GitHub release URLs used' (@($global:urls | Where-Object { -not $_.StartsWith($base) }).Count -eq 0)
Remove-Item (Split-Path $exe) -Recurse -Force -ErrorAction SilentlyContinue

# 2. RetroBat: launcher with the fixed template (TH-30)
$rb = New-RetroBat 'RetroBat'
Invoke-Install @{ Version = '1.2.3'; RetroBatPath = $rb }
$launcher = Join-Path $rb 'roms\windows\Mothdex.bat'
Check 'retrobat install succeeds' (-not $script:threw)
Check 'launcher written' (Test-Path $launcher)
$lines = @(Get-Content $launcher)
Check 'launcher template' (($lines.Count -eq 4) -and ($lines[0] -ceq '@echo off') -and ($lines[1] -ceq 'rem mothdex-launcher v1') -and
  ($lines[2] -ceq 'setlocal DisableDelayedExpansion') -and ($lines[3] -ceq "`"$exe`" tui %*"))
Check 'no temp launcher left' (@(Get-ChildItem (Split-Path $launcher) -Force -Filter '.Mothdex*').Count -eq 0)

# 3. a user's launcher without the marker is kept; ours is refreshed (TH-32)
Set-Content -Path $launcher -Value 'echo mine'
Invoke-Install @{ Version = '1.2.3'; RetroBatPath = $rb }
Check 'user launcher kept' ((Get-Content $launcher -Raw).Trim() -ceq 'echo mine')
Set-Content -Path $launcher -Value @('@echo off', 'rem mothdex-launcher v1', 'echo stale')
Invoke-Install @{ Version = '1.2.3'; RetroBatPath = $rb }
Check 'marked launcher refreshed' ((Get-Content $launcher -Raw) -match 'tui %\*')

# 4. unsafe path characters: binary installed, launcher skipped (TH-30)
$rb2 = New-RetroBat 'RetroBat2'
$odd = Join-Path $root 'a&b'
Invoke-Install @{ Version = '1.2.3'; RetroBatPath = $rb2; BinDir = $odd }
Check 'binary installed in odd path' (Test-Path (Join-Path $odd 'mothdex.exe'))
Check 'launcher skipped for odd path' (-not (Test-Path (Join-Path $rb2 'roms\windows\Mothdex.bat')))

# 5. folder outside LocalAppData gets an explicit ACL (TH-31)
$acl = (& icacls.exe $odd) -join "`n"
Check 'inheritance removed' (-not ($acl -match '\(I\)'))
Check 'Users have no write' (-not ($acl -match 'BUILTIN\\Users:[^\n]*\((F|M|W)\)'))
Check 'Authenticated Users and Everyone absent' (-not ($acl -match 'Authenticated Users|Everyone'))

# 6. checksum mismatch installs nothing (TH-33)
$target = Join-Path $root 'bad'
Invoke-Install @{ Version = 'v1.1.0'; BinDir = $target }
Check 'mismatch aborts' ($script:threw -and $script:message -match 'SHA-256 mismatch')
Check 'nothing installed' (-not (Test-Path (Join-Path $target 'mothdex.exe')))

# 7. version validation and latest
foreach ($bad in '1.2.3; calc', '..\..\x', '1.2', 'v1.2.3/../v2') {
  Invoke-Install @{ Version = $bad; BinDir = (Join-Path $root 'badver') }
  Check "refuses version '$bad'" $script:threw
}
Check 'no binary after bad versions' (-not (Test-Path (Join-Path $root 'badver\mothdex.exe')))
Invoke-Install @{ BinDir = (Join-Path $root 'latest') }
Check 'latest resolves to 1.2.3' ((-not $script:threw) -and (Test-Path (Join-Path $root 'latest\mothdex.exe')) -and
  (@($global:urls | Where-Object { $_ -like '*/download/v1.2.3/*' }).Count -ge 2))

# 8. dry run and uninstall
$rb3 = New-RetroBat 'RetroBat3'
$dry = Join-Path $root 'dry'
Invoke-Install @{ Version = '1.2.3'; RetroBatPath = $rb3; BinDir = $dry; DryRun = $true }
Check 'dry run writes nothing' ((-not (Test-Path $dry)) -and (-not (Test-Path (Join-Path $rb3 'roms\windows\Mothdex.bat'))))
Invoke-Install @{ Version = '1.2.3'; RetroBatPath = $rb3; BinDir = (Join-Path $root 'un') }
Invoke-Install @{ RetroBatPath = $rb3; BinDir = (Join-Path $root 'un'); Uninstall = $true }
Check 'uninstall removes binary and launcher' ((-not (Test-Path (Join-Path $root 'un\mothdex.exe'))) -and
  (-not (Test-Path (Join-Path $rb3 'roms\windows\Mothdex.bat'))))

# 9. cosign present: identity pinned to the tag, failure aborts
$tools = Join-Path $root 'tools'
New-Item -ItemType Directory -Path $tools | Out-Null
$cosignLog = Join-Path $root 'cosign.log'
Set-Content -Path (Join-Path $tools 'cosign.cmd') -Value @('@echo off', "echo %* >> `"$cosignLog`"", 'exit /b %COSIGN_RC%')
$oldPath = $env:PATH
$env:PATH = "$tools;$oldPath"
$env:COSIGN_RC = '0'
Invoke-Install @{ Version = 'v1.2.3'; BinDir = (Join-Path $root 'signed') }
$log = Get-Content $cosignLog -Raw
Check 'cosign verified' ((-not $script:threw) -and ($log -match 'verify-blob'))
Check 'cosign identity pinned' ($log -match [regex]::Escape('--certificate-identity https://github.com/Or4cu1o/mothdex-cli/.github/workflows/release.yml@refs/tags/v1.2.3'))
$env:COSIGN_RC = '1'
Invoke-Install @{ Version = 'v1.2.3'; BinDir = (Join-Path $root 'unsigned') }
Check 'failed signature aborts' ($script:threw -and (-not (Test-Path (Join-Path $root 'unsigned\mothdex.exe'))))
$env:PATH = $oldPath

Remove-Item -LiteralPath $root -Recurse -Force -ErrorAction SilentlyContinue
Write-Output "$($script:pass) passed, $($script:fail) failed"
exit ([int]($script:fail -gt 0))
