### Fixed

- T-72 (High, DLL planting; Sentinela T-70, IC-8): on Windows `mothdex-gui.exe` calls `SetDefaultDllDirectories` with the application folder
  and System32 before it loads `SDL2.dll`, and does not load it if the call fails. `LoadLibraryEx` with those flags already protected
  `SDL2.dll`, but SDL loads `vulkan-1`, `opengl32`, `XInput` and `hid` later with a plain `LoadLibrary`, which searches the working
  directory and `PATH`. `TestPlainLoadLibraryIgnoresThePathAndTheWorkingFolderOnceSDLIsOpen` plants a DLL in both and checks that a plain
  load does not find it while a DLL beside the executable and a System32 DLL the process does not already hold (`winhttp.dll`) still load;
  the Windows job of `gui-spike.yml` runs it as its own named step. A failure to restrict the search path has its own error, `ErrSearchPath`,
  not `SDL2 was not found`. IC-13 (the diagnostics line) is in T-73.
