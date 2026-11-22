### Added

- T-80 (Medium, GUI): the controller notices of `docs/ux/gui.md` 2.3 and the controller name and GUID in Settings › About (copy by Bússola in four
  languages; spec in cli#102).
  - The SDL backend now tracks every joystick, mapped or not (`SDL_JOYDEVICEADDED` and `REMOVED`, `SDL_JoystickNameForIndex`,
    `SDL_JoystickGetDeviceInstanceID`, `SDL_JoystickGetDeviceGUID`), and reports the whole list at start (also when it is empty) and on each plug or
    unplug (`input.IncomingPadList`, `gui.PadsMsg`). The two new events are in the constants check. The GUID is a 16-byte struct that purego can
    return only on amd64 and arm64, so on 32-bit ARM and x86 it stays unbound and About says `GUID: Unavailable on this system`.
  - With no controller SDL can use, a warn toast says `Connect a controller. On a keyboard: arrows, Enter, Esc.` once when that state appears
    (after the first report, and when the last pad goes away), and the top bar keeps the badge `No controller` (never cut) for as long as it lasts.
  - A joystick SDL has no mapping for gets a warn toast once per device per session that names it and points to Settings › About; a long name is the
    only thing cut. Notices show one at a time; one that comes during the Splash or while another toast is on screen waits.
  - About lists each joystick (`Controller: <name>`, the whole path of the `gamecontrollerdb.txt` to add a mapping to for an unknown one, `GUID:
    <guid>`), or `None connected`.
  - 6 render scenes (the toast and badge, in ru_ru, a long device name, About with controllers and without).
