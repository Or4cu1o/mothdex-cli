<#
  Tests for install.ps1 on Windows (run by the `installer-windows` CI job; needs no network).
  Invoke-WebRequest is shadowed by a function that serves a local fake release and refuses any other URL, and the
  "binary" in the fake zips is a copy of whoami.exe. Covers launcher quoting (TH-30), ACL (TH-31), marker and atomic
  launcher (TH-32) and verification (TH-33), and the Ports part (ports.md, T-70 IC-1 to IC-11): GUI archive, probe,
  launcher v2, default flip, upgrade and uninstall. The fake mothdex-gui.exe files are compiled with csc.exe.
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
$global:fakeHttpUrl = $null
$global:fakeNoFinal = $false
$global:fakeRelease = $release
$global:refreshCalls = New-Object System.Collections.ArrayList
$global:refreshFails = $false

function Check([string]$What, [bool]$Ok) {
  if ($Ok) { $script:pass++ } else { $script:fail++; Write-Output "FAIL: $What [last error: $($script:message)]" }
}

# Network double: only release URLs of Or4cu1o/mothdex-cli, served from $release.
function Invoke-WebRequest {
  param([string]$Uri, [string]$OutFile, [switch]$UseBasicParsing, [int]$MaximumRedirection, [switch]$PassThru, [int]$TimeoutSec, [switch]$NoProxy)
  if ($Uri -ceq 'http://127.0.0.1:1234/reloadgames') {
    # the frontend refresh (IC-11): recorded, never served
    [void]$global:refreshCalls.Add(@{ Uri = $Uri; MaximumRedirection = $MaximumRedirection; TimeoutSec = $TimeoutSec; NoProxy = [bool]$NoProxy })
    if ($global:refreshFails) { throw 'connection refused' }
    return
  }
  [void]$global:urls.Add($Uri)
  if ($global:fakeHttpUrl) {
    # a REAL request to a local http listener: the response a redirect to http would produce on PowerShell 5.1 or 7
    return Microsoft.PowerShell.Utility\Invoke-WebRequest -Uri $global:fakeHttpUrl -OutFile $OutFile -UseBasicParsing -PassThru
  }
  if (-not $Uri.StartsWith($global:fakeBase, [StringComparison]::Ordinal)) { throw "BLOCKED URL $Uri" }
  $rel = $Uri.Substring($global:fakeBase.Length) -replace '^download/', '' -replace '^latest/download/', 'latest/'
  $src = Join-Path $global:fakeRelease ($rel -replace '/', '\')
  if (-not (Test-Path -LiteralPath $src)) { throw "404 $Uri" }
  Copy-Item -LiteralPath $src -Destination $OutFile
  if ($global:fakeNoFinal) { return [pscustomobject]@{ BaseResponse = [pscustomobject]@{ } } }
  if ($PassThru) { return [pscustomobject]@{ BaseResponse = [pscustomobject]@{ ResponseUri = [uri]$Uri } } }
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
  $script:output = ''
  $script:exitCode = 0
  $global:LASTEXITCODE = 0
  $global:urls.Clear()
  try { $script:output = (& $installer @Params *>&1 | Out-String); $script:exitCode = $LASTEXITCODE } catch { $script:threw = $true; $script:message = ($_.Exception.Message + " @ " + $_.InvocationInfo.PositionMessage + " | " + $_.Exception.GetType().FullName) }
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
Invoke-Install @{ Version = '1.2.3'; Launcher = $true; RetroBatPath = $rb }
$launcher = Join-Path $rb 'roms\windows\Mothdex.bat'
Check 'retrobat install succeeds' (-not $script:threw)
Check 'launcher written' (Test-Path $launcher)
$lines = @(Get-Content $launcher)
$guiPath = Join-Path (Split-Path $exe) 'mothdex-gui.exe'
Check 'launcher template (v2, no argument forwarding)' (($lines.Count -eq 8) -and ($lines[0] -ceq '@echo off') -and ($lines[1] -ceq 'rem mothdex-launcher v2') -and
  ($lines[2] -ceq 'setlocal DisableDelayedExpansion') -and ($lines[3] -ceq "if not exist `"$guiPath`" goto tui") -and ($lines[4] -ceq "`"$guiPath`"") -and
  ($lines[5] -ceq 'exit /b %errorlevel%') -and ($lines[6] -ceq ':tui') -and ($lines[7] -ceq "`"$exe`" tui"))
Check 'no temp launcher left' (@(Get-ChildItem (Split-Path $launcher) -Force -Filter '.Mothdex*').Count -eq 0)

# 2b. launcher is opt-in (D22): without -Launcher nothing is written to the RetroBat folder
$rbOff = New-RetroBat 'RetroBatOff'
Invoke-Install @{ Version = '1.2.3'; RetroBatPath = $rbOff; BinDir = (Join-Path $root 'off') }
Check 'no launcher by default' ((-not $script:threw) -and (-not (Test-Path (Join-Path $rbOff 'roms\windows\Mothdex.bat'))))

# 3. a user's launcher without the marker is kept; ours is refreshed (TH-32)
Set-Content -Path $launcher -Value 'echo mine'
Invoke-Install @{ Version = '1.2.3'; Launcher = $true; RetroBatPath = $rb }
Check 'user launcher kept' ((Get-Content $launcher -Raw).Trim() -ceq 'echo mine')
Set-Content -Path $launcher -Value @('@echo off', 'rem mothdex-launcher v1', 'echo stale')
Invoke-Install @{ Version = '1.2.3'; Launcher = $true; RetroBatPath = $rb }
Check 'marked launcher refreshed to v2' (((Get-Content $launcher -Raw) -match 'rem mothdex-launcher v2') -and ((Get-Content $launcher -Raw) -notmatch 'stale'))

# 4. unsafe path characters: binary installed, launcher skipped (TH-30)
$rb2 = New-RetroBat 'RetroBat2'
$odd = Join-Path $root 'a&b'
Invoke-Install @{ Version = '1.2.3'; Launcher = $true; RetroBatPath = $rb2; BinDir = $odd }
Check 'binary installed in odd path' (Test-Path (Join-Path $odd 'mothdex.exe'))
Check 'launcher skipped for odd path' (-not (Test-Path (Join-Path $rb2 'roms\windows\Mothdex.bat')))

# 5. folder outside LocalAppData gets an explicit ACL (TH-31)
$acl = (& icacls.exe $odd) -join "`n"
Check 'inheritance removed' (-not ($acl -match '\(I\)'))
Check 'Users have no write' (-not ($acl -match 'BUILTIN\\Users:[^\n]*\((F|M|W)\)'))
Check 'Authenticated Users and Everyone absent' (-not ($acl -match 'Authenticated Users|Everyone'))

# 5b. BinDir validation (TH-31): relative, '..', sibling with the LocalAppData prefix, foreign write ACE, junction
foreach ($badDir in 'relative\bin', ($root + '\x\..\..\y')) {
  Invoke-Install @{ Version = '1.2.3'; BinDir = $badDir }
  Check "refuses BinDir '$badDir'" $script:threw
}
$sibling = Join-Path $root 'Local2\bin'
Invoke-Install @{ Version = '1.2.3'; BinDir = $sibling }
$sibAcl = (& icacls.exe $sibling) -join "`n"
Check 'prefix sibling of LocalAppData gets the explicit ACL' ((-not $script:threw) -and (-not ($sibAcl -match '\(I\)')))
$shared = Join-Path $root 'shared'
New-Item -ItemType Directory -Path $shared | Out-Null
& icacls.exe $shared /grant '*S-1-5-32-545:(OI)(CI)M' | Out-Null
Invoke-Install @{ Version = '1.2.3'; BinDir = $shared }
Check 'existing folder with a foreign write ACE is refused' ($script:threw -and (-not (Test-Path (Join-Path $shared 'mothdex.exe'))))
$mine = Join-Path $root 'mine'
New-Item -ItemType Directory -Path $mine | Out-Null
$me = [Security.Principal.WindowsIdentity]::GetCurrent().User.Value
& icacls.exe $mine /inheritance:r /grant:r "*${me}:(OI)(CI)F" | Out-Null
Invoke-Install @{ Version = '1.2.3'; BinDir = $mine }
Check 'existing folder that only you can write is accepted' ((-not $script:threw) -and (Test-Path (Join-Path $mine 'mothdex.exe')))
$target2 = Join-Path $root 'junction-target'
New-Item -ItemType Directory -Path $target2 | Out-Null
$junction = Join-Path $root 'junction'
New-Item -ItemType Junction -Path $junction -Target $target2 | Out-Null
Invoke-Install @{ Version = '1.2.3'; BinDir = $junction }
Check 'junction BinDir is refused' ($script:threw -and (-not (Test-Path (Join-Path $target2 'mothdex.exe'))))

# 5c. generic inherit-only ACE (GA on Users) is refused; a download that ends on http is refused (real response)
$inherit = Join-Path $root 'inherit-only'
New-Item -ItemType Directory -Path $inherit | Out-Null
& icacls.exe $inherit /grant '*S-1-5-32-545:(OI)(IO)(GA)' | Out-Null
Invoke-Install @{ Version = '1.2.3'; BinDir = $inherit }
Check 'existing folder with an inherit-only generic-all ACE is refused' ($script:threw -and (-not (Test-Path (Join-Path $inherit 'mothdex.exe'))))

$tcp = [Net.Sockets.TcpListener]::new([Net.IPAddress]::Loopback, 0)
$tcp.Start(); $port = $tcp.LocalEndpoint.Port; $tcp.Stop()
$http = [Net.HttpListener]::new()
$http.Prefixes.Add("http://127.0.0.1:$port/")
$http.Start()
$serverShell = [PowerShell]::Create()
[void]$serverShell.AddScript({ param($l) for ($i = 0; $i -lt 4; $i++) { $c = $l.GetContext(); $b = [Text.Encoding]::ASCII.GetBytes('x'); $c.Response.OutputStream.Write($b, 0, 1); $c.Response.Close() } }).AddArgument($http)
$serverAsync = $serverShell.BeginInvoke()
$global:fakeHttpUrl = "http://127.0.0.1:$port/file"
Invoke-Install @{ Version = '1.2.3'; BinDir = (Join-Path $root 'viahttp') }
$global:fakeHttpUrl = $null
$global:fakeNoFinal = $false
Check 'a download that ended on http is refused' ($script:threw -and $script:message -match 'not https' -and (-not (Test-Path (Join-Path $root 'viahttp\mothdex.exe'))))
$http.Stop(); $serverShell.Dispose()
$global:fakeNoFinal = $true
Invoke-Install @{ Version = '1.2.3'; BinDir = (Join-Path $root 'nofinal') }
$global:fakeNoFinal = $false
Check 'an unreadable final address is refused (fail closed)' ($script:threw -and $script:message -match 'could not be read' -and (-not (Test-Path (Join-Path $root 'nofinal\mothdex.exe'))))

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
Invoke-Install @{ Version = '1.2.3'; Launcher = $true; RetroBatPath = $rb3; BinDir = $dry; DryRun = $true }
Check 'dry run writes nothing' ((-not (Test-Path $dry)) -and (-not (Test-Path (Join-Path $rb3 'roms\windows\Mothdex.bat'))))
Invoke-Install @{ Version = '1.2.3'; Launcher = $true; RetroBatPath = $rb3; BinDir = (Join-Path $root 'un') }
Invoke-Install @{ Launcher = $true; RetroBatPath = $rb3; BinDir = (Join-Path $root 'un'); Uninstall = $true }
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


# 10. Ports: GUI archive, eligibility, launcher v2, default flip (ports.md sections 2 and 5; T-70 IC-1 to IC-12).
# The fake mothdex-gui.exe files are compiled once with the .NET Framework compiler that ships with Windows.
$csc = Join-Path $env:WINDIR 'Microsoft.NET\Framework64\v4.0.30319\csc.exe'
function New-FakeExe([string]$Name, [string]$Source) {
  $cs = Join-Path $root "$Name.cs"
  $exeOut = Join-Path $root "$Name.exe"
  Set-Content -Path $cs -Value $Source
  & $csc /nologo /target:exe "/out:$exeOut" $cs | Out-Null
  if (-not (Test-Path -LiteralPath $exeOut)) { throw "could not compile $Name" }
  return $exeOut
}
$okSource = @'
using System; using System.IO; using System.Linq;
class P { static int Main(string[] a) {
  if (a.Length > 0 && a[0] == "--probe") {
    var tmp = Path.GetTempPath();
    File.WriteAllLines(Path.Combine(tmp, "mothdex-probe-env.txt"), Environment.GetEnvironmentVariables().Keys.Cast<object>().Select(k => k.ToString() + "=" + Environment.GetEnvironmentVariable(k.ToString())).ToArray());
    File.WriteAllText(Path.Combine(tmp, "mothdex-probe-where.txt"), System.Reflection.Assembly.GetExecutingAssembly().Location);
    Console.WriteLine("sdl 2.32.10"); return 0; }
  Console.WriteLine("gui"); return 0; } }
'@
$fakeGui = @{
  ok    = New-FakeExe 'gui-ok' $okSource
  no    = New-FakeExe 'gui-no' 'class P { static int Main(string[] a) { System.Console.Error.WriteLine("SDL2.dll not found"); return 1; } }'
  crash = New-FakeExe 'gui-crash' 'class P { static int Main(string[] a) { return 2; } }'
  hang  = New-FakeExe 'gui-hang' 'class P { static int Main(string[] a) { System.Threading.Thread.Sleep(120000); return 0; } }'
  long  = New-FakeExe 'gui-long' 'class P { static int Main(string[] a) { System.Console.Error.WriteLine("\u001b[31m" + new string((char)48, 300)); return 1; } }'
}

function New-Zip([string]$Path, [hashtable]$Entries, [string[]]$LinkEntries = @()) {
  $zipFile = [IO.Compression.ZipFile]::Open($Path, 'Create')
  try {
    foreach ($name in $Entries.Keys) {
      $entry = $zipFile.CreateEntry($name)
      if ($LinkEntries -contains $name) { $entry.ExternalAttributes = [BitConverter]::ToInt32([BitConverter]::GetBytes([uint32]2717908992), 0) }
      $value = $Entries[$name]
      if ($value -isnot [byte[]]) { $value = [Text.Encoding]::UTF8.GetBytes([string]$value) }
      $stream = $entry.Open()
      try { $stream.Write($value, 0, $value.Length) } finally { $stream.Dispose() }
    }
  } finally { $zipFile.Dispose() }
}

# New-GuiTag <tag> <variant>: a release like New-Tag plus a mothdex-gui_ archive for windows amd64.
# Variants: ok no crash long hang (probe behaviour), extra dotdot backslash symlink nomember badspace badstar (archive layout),
# missing (listed, not downloadable), tamper (checksum mismatch).
function New-GuiTag([string]$Tag, [string]$Variant) {
  New-Tag $Tag $false
  $ver = $Tag.TrimStart('v')
  $dir = Join-Path $release $Tag
  $kind = $Variant
  if (-not $fakeGui.ContainsKey($kind)) { $kind = 'ok' }
  $entries = @{
    'mothdex-gui.exe' = [IO.File]::ReadAllBytes($fakeGui[$kind])
    'SDL2.dll' = 'not a real dll'
    'LICENSE' = 'license'
    'THIRD_PARTY/NotoSans-OFL-1.1.txt' = 'noto'
    'THIRD_PARTY/SDL2-zlib.txt' = 'zlib'
  }
  $links = @()
  switch ($Variant) {
    'extra' { $entries['evil.exe'] = 'x' }
    'badspace' { $entries['THIRD_PARTY/a b.txt'] = 'x' }
    'badstar' { $entries['THIRD_PARTY/a*b.txt'] = 'x' }
    'dotdot' { $entries['../evil.exe'] = 'x' }
    'backslash' { $entries['THIRD_PARTY\evil.txt'] = 'x' }
    'symlink' { $links = @('SDL2.dll') }
    'nomember' { $entries.Remove('mothdex-gui.exe') }
  }
  $name = "mothdex-gui_${ver}_windows_amd64.zip"
  New-Zip (Join-Path $dir $name) $entries $links
  $hash = (Get-FileHash (Join-Path $dir $name) -Algorithm SHA256).Hash.ToLowerInvariant()
  Add-Content -Path (Join-Path $dir 'checksums.txt') -Value "$hash  $name"
  if ($Variant -eq 'missing') { Remove-Item (Join-Path $dir $name) }
  if ($Variant -eq 'tamper') { Add-Content -Path (Join-Path $dir $name) -Value 'tamper' }
}
New-GuiTag 'v1.3.0' 'ok'
New-GuiTag 'v1.3.1' 'no'
New-GuiTag 'v1.3.2' 'crash'
New-GuiTag 'v1.3.3' 'extra'
New-GuiTag 'v1.3.4' 'dotdot'
New-GuiTag 'v1.3.5' 'backslash'
New-GuiTag 'v1.3.6' 'symlink'
New-GuiTag 'v1.3.7' 'nomember'
New-GuiTag 'v1.3.8' 'missing'
New-GuiTag 'v1.3.9' 'tamper'
New-GuiTag 'v1.4.0' 'long'
New-GuiTag 'v1.4.1' 'badspace'
New-GuiTag 'v1.4.2' 'badstar'
New-GuiTag 'v1.4.3' 'hang'

$tempDir = [IO.Path]::GetTempPath()
function Clear-ProbeFiles { Remove-Item -LiteralPath (Join-Path $tempDir 'mothdex-probe-env.txt'), (Join-Path $tempDir 'mothdex-probe-where.txt') -Force -ErrorAction SilentlyContinue }
function Lines([string]$Path) { return @(Get-Content -LiteralPath $Path) }

# the default flip: the release lists a GUI archive, so the launcher is on without -Launcher (IC-3, D22)
Clear-ProbeFiles
$rbG = New-RetroBat 'RetroBatG'
$g1 = Join-Path $root 'g1'
$env:SDL_VIDEODRIVER = 'x'; $env:MOTHDEX_CONFIG = 'y'
$global:refreshCalls.Clear()
Invoke-Install @{ Version = 'v1.3.0'; RetroBatPath = $rbG; BinDir = $g1 }
Remove-Item Env:SDL_VIDEODRIVER, Env:MOTHDEX_CONFIG
$portBat = Join-Path $rbG 'roms\windows\Mothdex.bat'
Check 'GUI install succeeds' (-not $script:threw)
Check 'GUI and SDL2.dll installed beside the CLI' ((Test-Path (Join-Path $g1 'mothdex-gui.exe')) -and (Test-Path (Join-Path $g1 'SDL2.dll')))
Check 'notices installed read-only' ((Test-Path (Join-Path $g1 'mothdex-licenses\NotoSans-OFL-1.1.txt')) -and (Test-Path (Join-Path $g1 'mothdex-licenses\SDL2-zlib.txt')) -and
  (Get-Item (Join-Path $g1 'mothdex-licenses\SDL2-zlib.txt')).IsReadOnly)
Check 'no staging leftovers in BinDir' (@(Get-ChildItem $g1 -Force | Where-Object { $_.Name -like '.mothdex*' }).Count -eq 0)
Check 'launcher on by default for a release with a GUI archive' (Test-Path $portBat)
$expect = @('@echo off', 'rem mothdex-launcher v2', 'setlocal DisableDelayedExpansion', "if not exist `"$g1\mothdex-gui.exe`" goto tui",
  "`"$g1\mothdex-gui.exe`"", 'exit /b %errorlevel%', ':tui', "`"$g1\mothdex.exe`" tui")
$got = Lines $portBat
Check 'launcher content equals the v2 template' (($got.Count -eq $expect.Count) -and (@(0..($expect.Count - 1) | Where-Object { $got[$_] -cne $expect[$_] }).Count -eq 0))
Check 'launcher has no percent-star (IC-6)' (-not ((Get-Content $portBat -Raw).Contains('%*')))
Check 'launcher is CRLF without a BOM' (([IO.File]::ReadAllBytes($portBat)[0] -eq 64) -and ((Get-Content $portBat -Raw).Contains("`r`n")))
$probeEnv = Lines (Join-Path $tempDir 'mothdex-probe-env.txt')
Check 'probe saw SystemRoot, PATH and TEMP only (IC-1)' (@($probeEnv | Where-Object { $_ -match '^(SDL_|LD_|MOTHDEX_|USERPROFILE|LOCALAPPDATA|APPDATA|HOME)' }).Count -eq 0)
Check 'probe PATH is System32 only' ($probeEnv -contains ('PATH=' + (Join-Path $env:SystemRoot 'System32')))
$where = (Get-Content (Join-Path $tempDir 'mothdex-probe-where.txt') -Raw).Trim()
Check 'probe ran from a staging folder inside BinDir (IC-2)' ($where.StartsWith((Join-Path $g1 '.mothdex-gui.new.'), [StringComparison]::OrdinalIgnoreCase))
Check 'frontend refresh asked once (IC-11)' ($global:refreshCalls.Count -eq 1)
$call = $global:refreshCalls[0]
Check 'refresh: literal URL, no redirect, 3 seconds, no proxy (IC-11)' (($call.Uri -ceq 'http://127.0.0.1:1234/reloadgames') -and ($call.MaximumRedirection -eq 0) -and ($call.TimeoutSec -eq 3) -and $call.NoProxy)
Check 'path and opt-out printed (IC-12)' ($script:output -match 'opt out with -NoLauncher')
Check 'only release URLs downloaded' (@($global:urls | Where-Object { -not $_.StartsWith($base) }).Count -eq 0)

# a failing refresh is not fatal
$global:refreshFails = $true
Invoke-Install @{ Version = 'v1.3.0'; RetroBatPath = $rbG; BinDir = $g1 }
$global:refreshFails = $false
Check 'a failing refresh does not fail the install' ((-not $script:threw) -and ($script:exitCode -eq 0) -and ($script:output -match 'could not ask RetroBat'))

# IC-7: no launcher in a roms\windows that other users can write to
$rbU = New-RetroBat 'RetroBatUsers'
& icacls.exe (Join-Path $rbU 'roms\windows') /grant '*S-1-5-32-545:(OI)(CI)M' | Out-Null
Invoke-Install @{ Version = 'v1.3.0'; RetroBatPath = $rbU; BinDir = (Join-Path $root 'g2') }
Check 'GUI installed even when the launcher folder is not trusted' ((-not $script:threw) -and (Test-Path (Join-Path $root 'g2\mothdex-gui.exe')))
Check 'launcher skipped where Users can write (IC-7)' (-not (Test-Path (Join-Path $rbU 'roms\windows\Mothdex.bat')))
Check 'folder ACL not rewritten' (((& icacls.exe (Join-Path $rbU 'roms\windows')) -join "`n") -match 'BUILTIN\\Users:[^\n]*\(M\)')

# -NoLauncher and conflicting flags
$rbN = New-RetroBat 'RetroBatN'
Invoke-Install @{ Version = 'v1.3.0'; RetroBatPath = $rbN; BinDir = (Join-Path $root 'g3'); NoLauncher = $true }
Check '-NoLauncher: GUI installed, no launcher' ((Test-Path (Join-Path $root 'g3\mothdex-gui.exe')) -and (-not (Test-Path (Join-Path $rbN 'roms\windows\Mothdex.bat'))))
Invoke-Install @{ Version = 'v1.3.0'; BinDir = (Join-Path $root 'g4'); Gui = $true; NoGui = $true }
Check '-Gui with -NoGui is refused' ($script:threw -and ($script:message -match 'cannot be used together'))
Invoke-Install @{ Version = 'v1.3.0'; BinDir = (Join-Path $root 'g4'); Launcher = $true; NoLauncher = $true }
Check '-Launcher with -NoLauncher is refused' $script:threw

# IC-9 and IC-10: a probe that fails installs neither the GUI nor SDL2.dll; the CLI install completes and the exit code is 1
foreach ($pair in @(@('v1.3.1', 'no'), @('v1.3.2', 'crash'), @('v1.4.0', 'long'))) {
  $dir = Join-Path $root "p-$($pair[1])"
  $rbP = New-RetroBat "RetroBatP$($pair[1])"
  Invoke-Install @{ Version = $pair[0]; RetroBatPath = $rbP; BinDir = $dir }
  Check "probe '$($pair[1])': the CLI install completes" ((-not $script:threw) -and (Test-Path (Join-Path $dir 'mothdex.exe')))
  Check "probe '$($pair[1])': neither the GUI nor SDL2.dll installed" ((-not (Test-Path (Join-Path $dir 'mothdex-gui.exe'))) -and (-not (Test-Path (Join-Path $dir 'SDL2.dll'))))
  Check "probe '$($pair[1])': reported as an error, exit code 1" (($script:output -match 'mothdex-gui.exe --probe failed') -and ($script:exitCode -eq 1))
  Check "probe '$($pair[1])': staging folder removed" (@(Get-ChildItem $dir -Force | Where-Object { $_.Name -like '.mothdex*' }).Count -eq 0)
  Check "probe '$($pair[1])': the launcher falls back to tui" ((Test-Path (Join-Path $rbP 'roms\windows\Mothdex.bat')) -and (-not (Test-Path (Join-Path $dir 'mothdex-licenses'))))
}
Check 'probe message cut to 200 characters without control characters (IC-10)' (($script:output -notmatch [char]27) -and ($script:output -notmatch '0{201}'))

# IC-3: whether a GUI archive exists comes from the verified checksums.txt only
$rbL = New-RetroBat 'RetroBatL'
Invoke-Install @{ Version = 'v1.2.3'; RetroBatPath = $rbL; BinDir = (Join-Path $root 'l1') }
Check 'not listed: CLI only, with a note' ((-not $script:threw) -and (Test-Path (Join-Path $root 'l1\mothdex.exe')) -and ($script:output -match 'no GUI archive') -and
  (-not (Test-Path (Join-Path $root 'l1\mothdex-gui.exe'))))
Check 'not listed: the launcher stays opt-in' (-not (Test-Path (Join-Path $rbL 'roms\windows\Mothdex.bat')))
Check 'not listed: no GUI download attempted' (@($global:urls | Where-Object { $_ -like '*mothdex-gui_*' }).Count -eq 0)
Invoke-Install @{ Version = 'v1.2.3'; RetroBatPath = $rbL; BinDir = (Join-Path $root 'l1'); Gui = $true }
Check '-Gui before the GUI release does nothing' ((-not $script:threw) -and (-not (Test-Path (Join-Path $root 'l1\mothdex-gui.exe'))))
Invoke-Install @{ Version = 'v1.3.8'; BinDir = (Join-Path $root 'l2') }
Check 'listed but not downloadable aborts, nothing installed' ($script:threw -and (-not (Test-Path (Join-Path $root 'l2\mothdex.exe'))))
Invoke-Install @{ Version = 'v1.3.9'; BinDir = (Join-Path $root 'l3') }
Check 'a GUI archive failing its checksum aborts the whole install' ($script:threw -and ($script:message -match 'SHA-256 mismatch for mothdex-gui_') -and (-not (Test-Path (Join-Path $root 'l3\mothdex.exe'))))

# IC-4: exact member names, regular files only
foreach ($pair in @(@('v1.3.3', 'extra member'), @('v1.3.4', 'dotdot member'), @('v1.3.5', 'backslash member'), @('v1.3.6', 'link member'), @('v1.3.7', 'no executable'))) {
  $dir = Join-Path $root ("a-" + $pair[0])
  Invoke-Install @{ Version = $pair[0]; BinDir = $dir }
  Check "$($pair[1]): aborts, nothing installed" ($script:threw -and (-not (Test-Path (Join-Path $dir 'mothdex.exe'))) -and (-not (Test-Path (Join-Path $dir 'mothdex-gui.exe'))))
}
Check 'nothing written outside BinDir by a ../ member' (-not (Test-Path (Join-Path $root 'evil.exe')))

# IC-5: mothdex-licenses is never written or removed through a link
$g5 = Join-Path $root 'g5'
$victim = Join-Path $root 'victim'
New-Item -ItemType Directory -Path $g5, $victim | Out-Null
Set-Content -Path (Join-Path $victim 'keep.txt') -Value 'keep'
New-Item -ItemType Junction -Path (Join-Path $g5 'mothdex-licenses') -Target $victim | Out-Null
Invoke-Install @{ Version = 'v1.3.0'; BinDir = $g5 }
Check 'install completes, the linked notices folder is refused' ((-not $script:threw) -and (Test-Path (Join-Path $g5 'mothdex-gui.exe')))
Check 'nothing written through the junction' ((-not (Test-Path (Join-Path $victim 'SDL2-zlib.txt'))) -and (Test-Path (Join-Path $victim 'keep.txt')))
Invoke-Install @{ BinDir = $g5; Uninstall = $true }
Check 'uninstall does not follow the junction' ((Test-Path (Join-Path $victim 'keep.txt')) -and (Test-Path (Join-Path $g5 'mothdex-licenses')))


# IC-4: a member name outside A-Za-z0-9._- is refused as unsafe, not only as unknown
foreach ($pair in @(@('v1.4.1', 'space'), @('v1.4.2', 'star'))) {
  $dir = Join-Path $root ('bn-' + $pair[1])
  Invoke-Install @{ Version = $pair[0]; BinDir = $dir }
  Check "member name with a $($pair[1]) aborts as unsafe" ($script:threw -and ($script:message -match 'unsafe member name') -and (-not (Test-Path (Join-Path $dir 'mothdex.exe'))))
}

# IC-10: a probe that hangs is killed after 20 seconds and means not eligible (the staging folder can only be removed
# once the process is gone, so its absence proves the Kill)
$hangDir = Join-Path $root 'hang'
$rbH = New-RetroBat 'RetroBatHang'
Invoke-Install @{ Version = 'v1.4.3'; RetroBatPath = $rbH; BinDir = $hangDir }
Check 'a hanging probe: not eligible, exit code 0, CLI installed, no GUI' ((-not $script:threw) -and ($script:exitCode -eq 0) -and (Test-Path (Join-Path $hangDir 'mothdex.exe')) -and
  (-not (Test-Path (Join-Path $hangDir 'mothdex-gui.exe'))) -and (-not (Test-Path (Join-Path $hangDir 'SDL2.dll'))))
Check 'a hanging probe: says it did not finish' ($script:output -match 'did not finish within 20 seconds')
Check 'a hanging probe: the process was killed (staging folder removed)' (@(Get-ChildItem $hangDir -Force | Where-Object { $_.Name -like '.mothdex*' }).Count -eq 0)

# upgrade (5.5) and uninstall (5.6)
$rbV = New-RetroBat 'RetroBatV'
$g6 = Join-Path $root 'g6'
$v1Bat = Join-Path $rbV 'roms\windows\Mothdex.bat'
Set-Content -Path $v1Bat -Value @('@echo off', 'rem mothdex-launcher v1', 'echo old')
Invoke-Install @{ Version = 'v1.3.0'; RetroBatPath = $rbV; BinDir = $g6 }
Check 'a v1 launcher becomes v2' ((Get-Content $v1Bat -Raw) -match 'rem mothdex-launcher v2')
Set-Content -Path $v1Bat -Value 'echo mine'
Invoke-Install @{ Version = 'v1.3.0'; RetroBatPath = $rbV; BinDir = $g6 }
Check 'a file without a marker is kept' ((Get-Content $v1Bat -Raw).Trim() -ceq 'echo mine')
Remove-Item $v1Bat
Invoke-Install @{ Version = 'v1.3.0'; RetroBatPath = $rbV; BinDir = $g6; NoGui = $true }
Check '-NoGui removes the GUI, SDL2.dll and the notices' ((-not (Test-Path (Join-Path $g6 'mothdex-gui.exe'))) -and (-not (Test-Path (Join-Path $g6 'SDL2.dll'))) -and
  (-not (Test-Path (Join-Path $g6 'mothdex-licenses'))))
Check '-NoGui keeps the launcher (the default follows the release), which falls back to tui' ((Test-Path $v1Bat) -and ((Get-Content $v1Bat -Raw) -match 'rem mothdex-launcher v2'))
Check '-NoGui does not download the GUI archive' (@($global:urls | Where-Object { $_ -like '*mothdex-gui_*' }).Count -eq 0)
Invoke-Install @{ Version = 'v1.3.0'; RetroBatPath = $rbV; BinDir = $g6 }
Set-Content -Path (Join-Path $g6 'mothdex-licenses\notes.txt') -Value 'mine'
New-Item -ItemType Directory -Path (Join-Path $root 'cfg') | Out-Null
Invoke-Install @{ RetroBatPath = $rbV; BinDir = $g6; Uninstall = $true; DryRun = $true }
Check 'uninstall dry run removes nothing' ((Test-Path (Join-Path $g6 'mothdex-gui.exe')) -and ($script:output -match 'Would remove'))
Invoke-Install @{ RetroBatPath = $rbV; BinDir = $g6; Uninstall = $true }
Check 'uninstall removes the binaries, SDL2.dll and the launcher' ((-not (Test-Path (Join-Path $g6 'mothdex.exe'))) -and (-not (Test-Path (Join-Path $g6 'mothdex-gui.exe'))) -and
  (-not (Test-Path (Join-Path $g6 'SDL2.dll'))) -and (-not (Test-Path $v1Bat)))
Check 'uninstall keeps a foreign file with a plain name in the notices folder, and the folder' ((Test-Path (Join-Path $g6 'mothdex-licenses\notes.txt')) -and (-not (Test-Path (Join-Path $g6 'mothdex-licenses\SDL2-zlib.txt'))))
$g7 = Join-Path $root 'g7'
New-Item -ItemType Directory -Path $g7 | Out-Null
Set-Content -Path (Join-Path $g7 'SDL2.dll') -Value 'someone else'
Invoke-Install @{ BinDir = $g7; Uninstall = $true }
Check 'a SDL2.dll without our mothdex-gui.exe is not removed' (Test-Path (Join-Path $g7 'SDL2.dll'))

# an older GUI never outlives a release whose GUI is not installed (Alicerce, PR-88): neither the exe, SDL2.dll nor the notices
foreach ($pair in @(@('v1.3.1', 'probe error', 1), @('v1.2.3', 'not listed', 0))) {
  $upDir = Join-Path $root ('up-' + $pair[0])
  $rbUp = New-RetroBat ('RetroBatUp' + $pair[0])
  Invoke-Install @{ Version = 'v1.3.0'; RetroBatPath = $rbUp; BinDir = $upDir }
  Check "$($pair[1]): the first install has the GUI" ((Test-Path (Join-Path $upDir 'mothdex-gui.exe')) -and (Test-Path (Join-Path $upDir 'SDL2.dll')))
  Invoke-Install @{ Version = $pair[0]; RetroBatPath = $rbUp; BinDir = $upDir; DryRun = $true }
  Check "$($pair[1]): dry run prints the removal and removes nothing" (($script:output -match 'Would remove .*mothdex-gui.exe and SDL2.dll') -and (Test-Path (Join-Path $upDir 'mothdex-gui.exe')))
  Invoke-Install @{ Version = $pair[0]; RetroBatPath = $rbUp; BinDir = $upDir }
  Check "$($pair[1]): exit code $($pair[2])" ($script:exitCode -eq $pair[2])
  Check "$($pair[1]): the older GUI, SDL2.dll and notices are gone" ((-not (Test-Path (Join-Path $upDir 'mothdex-gui.exe'))) -and (-not (Test-Path (Join-Path $upDir 'SDL2.dll'))) -and
    (-not (Test-Path (Join-Path $upDir 'mothdex-licenses'))))
  Check "$($pair[1]): said so" ($script:output -match 'Removed .*mothdex-gui.exe and SDL2.dll')
  Check "$($pair[1]): the CLI of the new release is installed" (Test-Path (Join-Path $upDir 'mothdex.exe'))
}

# dry run (5.1)
$rbD = New-RetroBat 'RetroBatD'
$g8 = Join-Path $root 'g8'
New-Item -ItemType Directory -Path $g8 | Out-Null
Invoke-Install @{ Version = 'v1.3.0'; RetroBatPath = $rbD; BinDir = $g8; DryRun = $true }
Check 'dry run names the GUI archive, the eligibility and the launcher path' (($script:output -match 'GUI archive: mothdex-gui_1.3.0_windows_amd64.zip') -and ($script:output -match 'GUI eligibility: eligible') -and
  ($script:output -match 'Would write:'))
Check 'dry run changes nothing' ((@(Get-ChildItem $g8 -Force).Count -eq 0) -and (-not (Test-Path (Join-Path $rbD 'roms\windows\Mothdex.bat'))))

Remove-Item -LiteralPath $root -Recurse -Force -ErrorAction SilentlyContinue
Write-Output "$($script:pass) passed, $($script:fail) failed"
exit ([int]($script:fail -gt 0))
