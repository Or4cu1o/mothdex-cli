<#
  Tests for install.ps1 on Windows (run by the `installer-windows` CI job; needs no network).
  Invoke-WebRequest is shadowed by a function that serves a local fake release and refuses any other URL, and the
  "binary" in the fake zips is a copy of whoami.exe. Covers launcher quoting (TH-30), ACL (TH-31), marker and atomic
  launcher (TH-32) and verification (TH-33), and the Ports part (ports.md, T-70 IC-1 to IC-11): GUI archive, probe,
  launcher v3 (GUI only, D32), upgrade and uninstall. The fake mothdex-gui.exe files are compiled with csc.exe.
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

# 2. RetroBat, a release without a GUI archive: no launcher, even with -Launcher (D32: a Port has no terminal for tui)
$rb = New-RetroBat 'RetroBat'
Invoke-Install @{ Version = '1.2.3'; Launcher = $true; RetroBatPath = $rb }
$launcher = Join-Path $rb 'roms\windows\Mothdex.bat'
Check 'retrobat install succeeds' (-not $script:threw)
Check 'no launcher without the GUI, even with -Launcher' (-not (Test-Path $launcher))
Check 'and it says why' ($script:output -match 'written only when the GUI is installed')

# 2b. nothing is written to the RetroBat folder without the GUI, with or without the flag
$rbOff = New-RetroBat 'RetroBatOff'
Invoke-Install @{ Version = '1.2.3'; RetroBatPath = $rbOff; BinDir = (Join-Path $root 'off') }
Check 'no launcher by default without the GUI' ((-not $script:threw) -and (-not (Test-Path (Join-Path $rbOff 'roms\windows\Mothdex.bat'))))

# 3. without the GUI: a user's launcher without the marker is never touched; a launcher of ours is removed (TH-32, D32)
Set-Content -Path $launcher -Value 'echo mine'
Invoke-Install @{ Version = '1.2.3'; Launcher = $true; RetroBatPath = $rb }
Check 'user launcher kept' ((Get-Content $launcher -Raw).Trim() -ceq 'echo mine')
foreach ($old in 'v1', 'v2', 'v3') {
  Set-Content -Path $launcher -Value @('@echo off', "rem mothdex-launcher $old", 'echo stale')
  Invoke-Install @{ Version = '1.2.3'; Launcher = $true; RetroBatPath = $rb }
  Check "a $old launcher of ours is removed when the GUI is not installed" ((-not (Test-Path $launcher)) -and ($script:output -match 'Removed .*Mothdex.bat'))
}
Check 'the refresh asked RetroBat to reload after the removal' ($global:refreshCalls.Count -ge 1)

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
Set-Content -Path (Join-Path $rb3 'roms\windows\Mothdex.bat') -Value @('@echo off', 'rem mothdex-launcher v2', 'echo old')
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


# 10. Ports: GUI archive, eligibility, launcher v3 (GUI only, D32) (ports.md sections 2 and 5; T-70 IC-1 to IC-12).
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
    'THIRD_PARTY/Handjet-OFL-1.1.txt' = 'handjet'
    'THIRD_PARTY/SDL2-zlib.txt' = 'zlib'
    'THIRD_PARTY/README.md' = 'readme'
    'THIRD_PARTY/go-modules-mothdex.txt' = 'gomods'
    'THIRD_PARTY/go-modules-mothdex-gui.txt' = 'gomodsgui'
  }
  $links = @()
  switch ($Variant) {
    'extra' { $entries['evil.exe'] = 'x' }
    'badspace' { $entries['THIRD_PARTY/a b.txt'] = 'x' }
    'badstar' { $entries['THIRD_PARTY/a*b.txt'] = 'x' }
    'unknownnotice' { $entries['THIRD_PARTY/extra.txt'] = 'x' }
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
New-GuiTag 'v1.4.4' 'unknownnotice'

$tempDir = [IO.Path]::GetTempPath()
function Clear-ProbeFiles { Remove-Item -LiteralPath (Join-Path $tempDir 'mothdex-probe-env.txt'), (Join-Path $tempDir 'mothdex-probe-where.txt') -Force -ErrorAction SilentlyContinue }
function Lines([string]$Path) { return @(Get-Content -LiteralPath $Path) }

# the launcher is on whenever the GUI is installed, without -Launcher (D32)
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
Check 'launcher on by default when the GUI is installed' (Test-Path $portBat)
$expect = @('@echo off', 'rem mothdex-launcher v3', 'setlocal DisableDelayedExpansion', "`"$g1\mothdex-gui.exe`"", 'exit /b %errorlevel%')
$got = Lines $portBat
Check 'launcher content equals the v3 template (the GUI only, no tui fallback)' (($got.Count -eq $expect.Count) -and (@(0..($expect.Count - 1) | Where-Object { $got[$_] -cne $expect[$_] }).Count -eq 0))
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
  Check "probe '$($pair[1])': no launcher (a Port has no terminal for tui) and no notices" ((-not (Test-Path (Join-Path $rbP 'roms\windows\Mothdex.bat'))) -and (-not (Test-Path (Join-Path $dir 'mothdex-licenses'))))
}
Check 'probe message cut to 200 characters without control characters (IC-10)' (($script:output -notmatch [char]27) -and ($script:output -notmatch '0{201}'))

# IC-3: whether a GUI archive exists comes from the verified checksums.txt only
$rbL = New-RetroBat 'RetroBatL'
Invoke-Install @{ Version = 'v1.2.3'; RetroBatPath = $rbL; BinDir = (Join-Path $root 'l1') }
Check 'not listed: CLI only, with a note' ((-not $script:threw) -and (Test-Path (Join-Path $root 'l1\mothdex.exe')) -and ($script:output -match 'no GUI archive') -and
  (-not (Test-Path (Join-Path $root 'l1\mothdex-gui.exe'))))
Check 'not listed: no launcher' (-not (Test-Path (Join-Path $rbL 'roms\windows\Mothdex.bat')))
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

# IC-4: a safe name that is not a shipped notice is refused as unexpected (the allowlist)
$dirU = Join-Path $root 'bn-unknown'
Invoke-Install @{ Version = 'v1.4.4'; BinDir = $dirU }
Check 'a safe but unknown THIRD_PARTY name aborts and says the installer may be older than the release' ($script:threw -and ($script:message -match 'installer may be older than the release') -and ($script:message -match 'fetch install.ps1 from the release tag') -and (-not (Test-Path (Join-Path $dirU 'mothdex.exe'))))

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
Check 'a v1 launcher becomes v3' ((Get-Content $v1Bat -Raw) -match 'rem mothdex-launcher v3')
Set-Content -Path $v1Bat -Value 'echo mine'
Invoke-Install @{ Version = 'v1.3.0'; RetroBatPath = $rbV; BinDir = $g6 }
Check 'a file without a marker is kept' ((Get-Content $v1Bat -Raw).Trim() -ceq 'echo mine')
Remove-Item $v1Bat
Invoke-Install @{ Version = 'v1.3.0'; RetroBatPath = $rbV; BinDir = $g6 }
Check 'a deleted launcher comes back when the GUI is installed' ((Get-Content $v1Bat -Raw) -match 'rem mothdex-launcher v3')
Invoke-Install @{ Version = 'v1.3.0'; RetroBatPath = $rbV; BinDir = $g6; NoGui = $true }
Check '-NoGui removes the GUI, SDL2.dll and the notices' ((-not (Test-Path (Join-Path $g6 'mothdex-gui.exe'))) -and (-not (Test-Path (Join-Path $g6 'SDL2.dll'))) -and
  (-not (Test-Path (Join-Path $g6 'mothdex-licenses'))))
Check '-NoGui removes the launcher of ours (a Port has no terminal for tui)' ((-not (Test-Path $v1Bat)) -and ($script:output -match 'Removed .*Mothdex.bat'))
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

# D32 (PRT-G8): the three ways of not installing the GUI (a failing probe, no GUI archive, -NoGui): a launcher of ours goes, a file
# without our marker never does
$d32 = @(@{ v = 'v1.3.1'; extra = @{}; what = 'the probe failed' }, @{ v = 'v1.2.3'; extra = @{}; what = 'no GUI archive' }, @{ v = 'v1.3.0'; extra = @{ NoGui = $true }; what = '-NoGui' })
$d32i = 0
foreach ($c in $d32) {
  $d32i++
  $rbX = New-RetroBat "RetroBatD32-$d32i"
  $batX = Join-Path $rbX 'roms\windows\Mothdex.bat'
  $binX = Join-Path $root "d32-$d32i"
  foreach ($marker in 'v1', 'v2', 'v3') {
    Set-Content -Path $batX -Value @('@echo off', "rem mothdex-launcher $marker", 'echo old')
    $params = @{ Version = $c.v; RetroBatPath = $rbX; BinDir = $binX }
    foreach ($k in $c.extra.Keys) { $params[$k] = $c.extra[$k] }
    Invoke-Install $params
    Check "$($c.what): a $marker launcher of ours is removed and the removal reported" ((-not (Test-Path $batX)) -and ($script:output -match 'Removed .*Mothdex.bat'))
    Check "$($c.what), $marker launcher: the CLI install is there" (Test-Path (Join-Path $binX 'mothdex.exe'))
  }
  Set-Content -Path $batX -Value 'echo mine'
  Invoke-Install $params
  Check "$($c.what): a launcher without our marker is never touched" ((Get-Content $batX -Raw).Trim() -ceq 'echo mine')
  Remove-Item $batX -Force
  Invoke-Install $params
  Check "$($c.what): a fresh install writes no launcher" (-not (Test-Path $batX))
}
# -NoLauncher without the GUI: a launcher of ours would start nothing, so it is still removed
$rbN2 = New-RetroBat 'RetroBatD32-nolauncher'
$batN2 = Join-Path $rbN2 'roms\windows\Mothdex.bat'
Set-Content -Path $batN2 -Value @('@echo off', 'rem mothdex-launcher v2', 'echo old')
Invoke-Install @{ Version = 'v1.3.0'; RetroBatPath = $rbN2; BinDir = (Join-Path $root 'd32-nl'); NoGui = $true; NoLauncher = $true }
Check '-NoLauncher without the GUI: a marked launcher is removed' ((-not (Test-Path $batN2)) -and ($script:output -match 'Removed .*Mothdex.bat'))
# a marked launcher that is a symbolic link is never touched on the removal path: the link stays and so does its target
$rbL2 = New-RetroBat 'RetroBatD32-link'
$batL2 = Join-Path $rbL2 'roms\windows\Mothdex.bat'
$victimBat = Join-Path $root 'victim-launcher.bat'
Set-Content -Path $victimBat -Value @('@echo off', 'rem mothdex-launcher v2', 'echo precious')
$linked = $false
try { New-Item -ItemType SymbolicLink -Path $batL2 -Target $victimBat | Out-Null; $linked = $true } catch { }
Check 'a symbolic link could be created for the link case (the runner is an administrator)' $linked
if ($linked) {
  Invoke-Install @{ Version = 'v1.3.0'; RetroBatPath = $rbL2; BinDir = (Join-Path $root 'd32-link'); NoGui = $true }
  Check 'a marked launcher that is a link is left alone (the link is still there)' ((Get-Item -LiteralPath $batL2 -Force).LinkType -eq 'SymbolicLink')
  Check 'and the target of the link is intact' ((Get-Content $victimBat -Raw) -match 'echo precious')
  Invoke-Install @{ Version = 'v1.3.1'; RetroBatPath = $rbL2; BinDir = (Join-Path $root 'd32-link') }
  Check 'also when the GUI is not eligible' (((Get-Item -LiteralPath $batL2 -Force).LinkType -eq 'SymbolicLink') -and ((Get-Content $victimBat -Raw) -match 'echo precious'))
}
# upgrade from the v2 template (with the tui fallback) while the GUI is installed: it becomes v3; -NoLauncher leaves it as it is
$rbY = New-RetroBat 'RetroBatD32-up'
$batY = Join-Path $rbY 'roms\windows\Mothdex.bat'
$binY = Join-Path $root 'd32-up'
$v2Text = @('@echo off', 'rem mothdex-launcher v2', 'setlocal DisableDelayedExpansion', "if not exist `"$binY\mothdex-gui.exe`" goto tui", "`"$binY\mothdex-gui.exe`"", 'exit /b %errorlevel%', ':tui', "`"$binY\mothdex.exe`" tui")
Set-Content -Path $batY -Value $v2Text
Invoke-Install @{ Version = 'v1.3.0'; RetroBatPath = $rbY; BinDir = $binY; NoLauncher = $true }
Check '-NoLauncher with the GUI: an existing v2 launcher is left as it is' (((Get-Content $batY -Raw) -match 'rem mothdex-launcher v2') -and ((Get-Content $batY -Raw) -match 'goto tui'))
Invoke-Install @{ Version = 'v1.3.0'; RetroBatPath = $rbY; BinDir = $binY }
Check 'a v2 launcher (with the tui fallback) becomes v3 when the GUI is installed' (((Get-Content $batY -Raw) -match 'rem mothdex-launcher v3') -and ((Get-Content $batY -Raw) -notmatch 'tui'))
# dry run: the launcher follows the GUI
Set-Content -Path $batY -Value $v2Text
Invoke-Install @{ Version = 'v1.3.1'; RetroBatPath = $rbY; BinDir = $binY; DryRun = $true }
Check 'dry run, probe failure: says it would remove the launcher and removes nothing' (($script:output -match 'Would remove .*Mothdex.bat') -and (Test-Path $batY))
Invoke-Install @{ Version = 'v1.3.0'; RetroBatPath = $rbY; BinDir = $binY; DryRun = $true }
Check 'dry run, eligible: says it would write the launcher' ($script:output -match 'Would write: .*Mothdex.bat')

# dry run (5.1)
$rbD = New-RetroBat 'RetroBatD'
$g8 = Join-Path $root 'g8'
New-Item -ItemType Directory -Path $g8 | Out-Null
Invoke-Install @{ Version = 'v1.3.0'; RetroBatPath = $rbD; BinDir = $g8; DryRun = $true }
Check 'dry run names the GUI archive, the eligibility and the launcher path' (($script:output -match 'GUI archive: mothdex-gui_1.3.0_windows_amd64.zip') -and ($script:output -match 'GUI eligibility: eligible') -and
  ($script:output -match 'Would write:'))
Check 'dry run changes nothing' ((@(Get-ChildItem $g8 -Force).Count -eq 0) -and (-not (Test-Path (Join-Path $rbD 'roms\windows\Mothdex.bat'))))

# 11. PATH, `irm | iex` safety and install state (T-110 TM-1, TM-3, TM-7, TM-8, TM-10, TM-11, TM-14).
# The user PATH of the machine running these tests is saved and restored.
$versionExe = New-FakeExe 'cli-ok' 'class P { static int Main(string[] a) { System.Console.WriteLine("mothdex 1.5.0"); return 0; } }'
New-Tag 'v1.5.0' $false
$cliStage = Join-Path $root 'stage-cli-ok'
New-Item -ItemType Directory -Path $cliStage | Out-Null
Copy-Item $versionExe (Join-Path $cliStage 'mothdex.exe')
$cliZip = Join-Path $release 'v1.5.0\mothdex_1.5.0_windows_amd64.zip'
Remove-Item $cliZip
[IO.Compression.ZipFile]::CreateFromDirectory($cliStage, $cliZip)
Set-Content -Path (Join-Path $release 'v1.5.0\checksums.txt') -Value "$((Get-FileHash $cliZip -Algorithm SHA256).Hash.ToLowerInvariant())  mothdex_1.5.0_windows_amd64.zip"

$envKey = [Microsoft.Win32.Registry]::CurrentUser.OpenSubKey('Environment', $true)
$noExpand = [Microsoft.Win32.RegistryValueOptions]::DoNotExpandEnvironmentNames
$savedPath = $envKey.GetValue('Path', $null, $noExpand)
$savedKind = $null
if ($null -ne $savedPath) { $savedKind = $envKey.GetValueKind('Path') }
$savedSessionPath = $env:Path
function Get-RawPath { return [string]$envKey.GetValue('Path', $null, $noExpand) }
function Set-RawPath([string]$Value) { $envKey.SetValue('Path', $Value, [Microsoft.Win32.RegistryValueKind]::ExpandString); $env:Path = $savedSessionPath }
function Count-Entry([string]$List, [string]$Entry) { return @(($List -split ';') | Where-Object { $_ -ieq $Entry }).Count }

try {
  $preset = '%USERPROFILE%\bin;C:\Other Tool\'
  foreach ($before in $preset, "$preset;") {
    Set-RawPath $before
    $p1 = Join-Path $root ('path1' + $before.Length)
    $st = Join-Path $p1 '.mothdex-install.json'
    Invoke-Install @{ Version = 'v1.5.0'; BinDir = $p1 }
    Check 'path: install succeeds' ((-not $script:threw) -and ($script:exitCode -eq 0))
    Check 'path: appended once, the rest byte for byte, %VAR% not expanded' ((Get-RawPath) -ceq "$before;$p1")
    Check 'path: the value kind stays REG_EXPAND_SZ' ($envKey.GetValueKind('Path') -eq [Microsoft.Win32.RegistryValueKind]::ExpandString)
    Check 'path: the current session has it' ((Count-Entry $env:Path $p1) -eq 1)
    Check 'path: mothdex runs by name in this window' ((Get-Command mothdex.exe -ErrorAction SilentlyContinue).Source -ieq (Join-Path $p1 'mothdex.exe'))
    Check 'path: final message' (($script:output -match 'mothdex 1\.5\.0 (installed in|instalado em)') -and ($script:output -match 'mothdex --version'))
    $bytes = [IO.File]::ReadAllBytes($st)
    $data = [IO.File]::ReadAllText($st) | ConvertFrom-Json
    Check 'state: no byte order mark' ($bytes[0] -eq 0x7B)
    Check 'state: records the entry' (($data.path_added -eq $true) -and ($data.path_kind -ceq 'user_env') -and ($data.path_value -ceq $p1) -and ($data.version -ceq '1.5.0'))
    $installedLine = @(Get-Content $st | Where-Object { $_ -match 'installed_at' })[0]
    Invoke-Install @{ Version = 'v1.5.0'; BinDir = $p1 }
    Check 'path: a reinstall does not duplicate' ((Count-Entry (Get-RawPath) $p1) -eq 1)
    Check 'state: a reinstall keeps installed_at and path_added' ((@(Get-Content $st) -contains $installedLine) -and ((Get-Content $st -Raw) -match '"path_added": true'))
    Invoke-Install @{ BinDir = $p1; Uninstall = $true; DryRun = $true }
    Check 'uninstall dry run keeps the entry and the state' (((Count-Entry (Get-RawPath) $p1) -eq 1) -and (Test-Path $st))
    Invoke-Install @{ BinDir = $p1; Uninstall = $true; NoModifyPath = $true }
    Check 'uninstall -NoModifyPath keeps the entry' ((Count-Entry (Get-RawPath) $p1) -eq 1)
    Invoke-Install @{ BinDir = $p1; Uninstall = $true }
    Check 'uninstall restores the previous value exactly' (((Get-RawPath) -ceq $before) -and (-not (Test-Path $st)) -and ((Count-Entry $env:Path $p1) -eq 0))
  }

  # already present (other case, trailing backslash): nothing added, nothing removed
  $p2 = Join-Path $root 'path2'
  Set-RawPath "$preset;$($p2.ToUpperInvariant())\"
  Invoke-Install @{ Version = 'v1.5.0'; BinDir = $p2 }
  Check 'already present: not duplicated' ((Get-RawPath) -ceq "$preset;$($p2.ToUpperInvariant())\")
  Check 'already present: state says nothing added' ((Get-Content (Join-Path $p2 '.mothdex-install.json') -Raw) -match '"path_added": false')
  Invoke-Install @{ BinDir = $p2; Uninstall = $true }
  Check 'already present: uninstall leaves the user entry' ((Get-RawPath) -ceq "$preset;$($p2.ToUpperInvariant())\")

  # -NoModifyPath: the PowerShell command is printed, nothing changes
  $p3 = Join-Path $root 'path3'
  Set-RawPath $preset
  Invoke-Install @{ Version = 'v1.5.0'; BinDir = $p3; NoModifyPath = $true }
  Check 'NoModifyPath: registry and session untouched' (((Get-RawPath) -ceq $preset) -and ((Count-Entry $env:Path $p3) -eq 0))
  Check 'NoModifyPath: the PowerShell command' $script:output.Contains("[Environment]::SetEnvironmentVariable(`"Path`", [Environment]::GetEnvironmentVariable(`"Path`",`"User`") + `";$p3`", `"User`")")
  Check 'NoModifyPath: never setx' ($script:output -notmatch 'setx')

  # started by CMD (powershell -File): the CMD command, and the exit code reaches the caller
  $p4 = Join-Path $root "path4 o'neil"
  $wrapper = Join-Path $root 'wrapper.ps1'
  Set-Content -Path $wrapper -Value @"
function Invoke-WebRequest { param([string]`$Uri, [string]`$OutFile, [switch]`$UseBasicParsing, [int]`$MaximumRedirection, [switch]`$PassThru)
  Copy-Item -LiteralPath (Join-Path '$release' ((`$Uri.Substring('$base'.Length) -replace '^download/', '') -replace '/', '\')) -Destination `$OutFile
  return [pscustomobject]@{ BaseResponse = [pscustomobject]@{ ResponseUri = [uri]`$Uri } } }
`$env:PROCESSOR_ARCHITECTURE = 'AMD64'
& '$installer' -Version v1.5.0 -BinDir "$p4" -NoModifyPath
exit `$LASTEXITCODE
"@
  $shellExe = (Get-Process -Id $PID).Path
  $fromCmd = (& cmd.exe /c "`"$shellExe`" -NoProfile -ExecutionPolicy Bypass -File `"$wrapper`"" | Out-String)
  Check 'from CMD: exit code 0' ($LASTEXITCODE -eq 0)
  Check 'from CMD: the CMD command, with the quote doubled' ($fromCmd.Contains("powershell -NoProfile -Command `"[Environment]::SetEnvironmentVariable('Path', [Environment]::GetEnvironmentVariable('Path','User') + ';$($p4 -replace "'", "''")', 'User')`""))

  # TM-11: a failed verification or a binary that does not run leaves PATH and state alone
  foreach ($tag in 'v1.1.0', 'v1.2.3') {
    $pf = Join-Path $root "path-fail-$tag"
    Invoke-Install @{ Version = $tag; BinDir = $pf }
    Check "$tag leaves the PATH and writes no state" (((Get-RawPath) -ceq $preset) -and (-not (Test-Path (Join-Path $pf '.mothdex-install.json'))))
  }

  # TM-7: a folder others can write to is never added
  $open = Join-Path $env:LOCALAPPDATA 'open-bin'
  New-Item -ItemType Directory -Path $open | Out-Null
  & icacls.exe $open /grant '*S-1-5-32-545:(OI)(CI)M' | Out-Null
  Invoke-Install @{ Version = 'v1.5.0'; BinDir = $open }
  Check 'a folder writable by Users is not added to PATH, and the command is printed' (((Get-RawPath) -ceq $preset) -and ($script:output -match 'copy and run'))

  # TM-14: a hostile state removes nothing
  $p5 = Join-Path $root 'path5'
  Invoke-Install @{ Version = 'v1.5.0'; BinDir = $p5 }
  $st5 = Join-Path $p5 '.mothdex-install.json'
  $good = Get-Content $st5 -Raw
  foreach ($bad in @(
      ($good -replace '"path_value": "[^\n]*",', '"path_value": "C:\\Other Tool\\",'),
      ($good -replace '"schema_version": 1', '"schema_version": 2'),
      ($good -replace '"dir": "[^\n]*",', '"dir": "C:\\Windows",'),
      'not json', '[1]')) {
    [IO.File]::WriteAllText($st5, $bad)
    Copy-Item $versionExe (Join-Path $p5 'mothdex.exe') -Force
    $now = Get-RawPath
    Invoke-Install @{ BinDir = $p5; Uninstall = $true }
    Check 'hostile state: uninstall succeeds, warns and leaves the PATH' ((-not $script:threw) -and ($script:output -match 'not a valid install state') -and ((Get-RawPath) -ceq $now))
  }
  Remove-Item $st5
  Invoke-Install @{ BinDir = $p5; Uninstall = $true }
  Check 'uninstall without state says it cannot know the PATH entry' ($script:output -match 'no install state')

  # TM-3 / TM-1: the text of the script, as `irm | iex` and as a script block
  $text = [IO.File]::ReadAllText($installer)
  Check 'install.ps1 is ASCII' ($text -notmatch '[^\x00-\x7F]')
  foreach ($pct in 10, 20, 30, 40, 50, 60, 70, 80, 90, 99) {
    $parseErrors = $null
    [void][Management.Automation.Language.Parser]::ParseInput($text.Substring(0, [int]($text.Length * $pct / 100)), [ref]$null, [ref]$parseErrors)
    Check "a download cut at $pct% is a parse error (nothing runs)" ($parseErrors.Count -gt 0)
  }
  $p6 = Join-Path $root 'path6'
  $threw = $false
  try { & ([scriptblock]::Create($text)) -Version v1.5.0 -BinDir $p6 -NoModifyPath -NoGui *> $null } catch { $threw = $true }
  Check 'script block form takes parameters' ((-not $threw) -and (Test-Path (Join-Path $p6 'mothdex.exe')) -and ($LASTEXITCODE -eq 0))

  $baseBefore = $base
  $tlsBefore = [Net.ServicePointManager]::SecurityProtocol
  Set-StrictMode -Off
  $ErrorActionPreference = 'Continue'
  $ProgressPreference = 'Continue'
  $global:LASTEXITCODE = 0
  $threw = $false
  $script:message = ''
  try { Invoke-Expression $text *> $null } catch { $threw = $true; $script:message = "$($_.Exception.Message) @ $($_.InvocationInfo.PositionMessage)" }
  $afterOk = @{ threw = $threw; code = $LASTEXITCODE; eap = $ErrorActionPreference; progress = $ProgressPreference }
  $strictOk = $true
  try { $null = $variableNeverSet } catch { $strictOk = $false }
  $global:fakeRelease = Join-Path $root 'nowhere'
  $threw = $false
  try { Invoke-Expression $text *> $null } catch { $threw = $true }
  $afterFail = @{ threw = $threw; code = $LASTEXITCODE; eap = $ErrorActionPreference; progress = $ProgressPreference }
  $global:fakeRelease = $release
  $ErrorActionPreference = 'Stop'
  Set-StrictMode -Version 2.0
  Check "iex success: session alive, exit status 0 (threw=$($afterOk.threw) code=$($afterOk.code))" ((-not $afterOk.threw) -and ($afterOk.code -eq 0))
  Check "iex failure: session alive, LASTEXITCODE not 0 (threw=$($afterFail.threw) code=$($afterFail.code))" ($afterFail.threw -and ($afterFail.code -ne 0))
  Check 'iex leaks no preference or strict mode' ($strictOk -and ($afterOk.eap -eq 'Continue') -and ($afterFail.eap -eq 'Continue') -and ($afterFail.progress -eq 'Continue'))
  Check 'iex leaks no function or variable' ((-not (Get-Command Fail, Get-Remote, Add-UserPathEntry -ErrorAction SilentlyContinue)) -and
    (-not (Get-Variable Repo, State, NoModifyPath, SignerIdentity -ErrorAction SilentlyContinue)) -and ($base -ceq $baseBefore))
  Check 'TLS settings are not weakened' (([int][Net.ServicePointManager]::SecurityProtocol -band [int]$tlsBefore) -eq [int]$tlsBefore)
} finally {
  if ($null -ne $savedPath) { $envKey.SetValue('Path', $savedPath, $savedKind) } else { $envKey.DeleteValue('Path', $false) }
  $envKey.Dispose()
  $env:Path = $savedSessionPath
}

Remove-Item -LiteralPath $root -Recurse -Force -ErrorAction SilentlyContinue
Write-Output "$($script:pass) passed, $($script:fail) failed"
exit ([int]($script:fail -gt 0))
