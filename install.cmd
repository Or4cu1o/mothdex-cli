@echo off
rem Mothdex installer for the Windows Command Prompt: runs install.ps1 of the same release in Windows PowerShell and
rem returns its exit status. Options are those of install.ps1 (-NoGui, -Version X.Y.Z, -BinDir "C:\Tools\mothdex", ...).
rem See docs/adr/ADR-0003-pipe-install.md. The options are never pasted into PowerShell code (T-110 TM-4): each one goes
rem into an environment variable, and PowerShell reads them as data.
setlocal DisableDelayedExpansion
set "MOTHDEX_ARG_COUNT=0"

:next
if [%1]==[] goto run
set "MOTHDEX_ARG_%MOTHDEX_ARG_COUNT%=%~1"
set /a MOTHDEX_ARG_COUNT+=1 >nul
shift
goto next

:run
rem A CMD window opened from PowerShell 7 inherits its module path, and Windows PowerShell then cannot load its own
rem modules (Get-FileHash goes missing). Empty, it rebuilds the default; setlocal gives the caller's value back.
set "PSModulePath="
powershell.exe -NoProfile -ExecutionPolicy Bypass -Command "$ErrorActionPreference = 'Stop'; if ([int][Net.ServicePointManager]::SecurityProtocol -ne 0) { [Net.ServicePointManager]::SecurityProtocol = [Net.ServicePointManager]::SecurityProtocol -bor 3072 }; $p = @{}; $n = [int]$env:MOTHDEX_ARG_COUNT; for ($i = 0; $i -lt $n; $i++) { $a = [string][Environment]::GetEnvironmentVariable('MOTHDEX_ARG_' + $i); $k = $a -replace '^-', ''; if (($a -ne ('-' + $k)) -or (-not $k)) { throw ('mothdex install: unexpected argument: ' + $a) }; if (@('Launcher', 'NoLauncher', 'Gui', 'NoGui', 'NoModifyPath', 'NoAlias', 'DryRun', 'Uninstall') -contains $k) { $p[$k] = $true } elseif ((@('Version', 'BinDir', 'RetroBatPath', 'Alias') -contains $k) -and ($i + 1 -lt $n)) { $i++; $p[$k] = [string][Environment]::GetEnvironmentVariable('MOTHDEX_ARG_' + $i) } else { throw ('mothdex install: unknown option or missing value: ' + $a) } }; Remove-Item -Path Env:MOTHDEX_ARG_*; & ([scriptblock]::Create((irm 'https://github.com/Or4cu1o/mothdex-cli/releases/latest/download/install.ps1'))) @p; exit $LASTEXITCODE"
exit /b %errorlevel%
