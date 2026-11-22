### Added

- T-67 (Medium, test and CI only), GUI render harness: `TestRenderScenes` (`internal/ui/gui/sdl`, tag `gui`, skipped unless `GUI_RENDER_DIR`
  is set) draws 16 screens (Home and its menu, Search, Results, Entry, the Queue with its sheet and details, the "Download anyway"
  row, sheet, dialogs and toast of T-56, the busy quit dialog) at 640x480, 1280x720 and 1920x1080 with the real SDL2 software renderer
  and the pinned Noto Sans, and writes them as PNG files with an `index.txt`. It compares nothing with a stored image. Each scene lists
  texts its screen must show (`expect`), so a focus or an order that drifts, or a build without the feature, fails the test instead of
  writing a PNG of the wrong screen; it also fails when SDL cannot draw or a frame is blank. The new workflow `gui-render.yml` runs it on every pull request (no `paths:` filter, job
  `render (linux/amd64, SDL2 software renderer, Noto Sans)`, so it can be a required check) and uploads the folder as the artifact
  `gui-renders`. Run it by hand: `GUI_RENDER_DIR=/dir SDL_VIDEODRIVER=dummy go test -tags gui -run TestRenderScenes ./internal/ui/gui/sdl`.
