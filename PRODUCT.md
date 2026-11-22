# Product

<!-- impeccable:product-schema 1 -->

Status: **approved by the owner (D30)** (T-81, brand session of Decisions D28 8b). The facts come from
`docs/PLAN-v0.md`, the owner's decisions D1-D30 and the UX docs.

## Platform

native: mothdex-gui is a full-screen SDL2 gamepad app for Linux handhelds, TV boxes and PCs, and Windows (RetroBat).
mothdex is the CLI, TUI and Shell. Not web, iOS or Android.

## Users

- People who keep a retro game library on an emulation system (Batocera, EmuELEC, RetroBat and others) and want to
  find and download games for it.
- In the GUI they sit on a couch with a pad in front of a TV, or hold a handheld with a 3.5-inch screen. There is no
  keyboard or mouse.
- In the CLI, TUI and Shell they work at a terminal, often over SSH into the same device.

## Product Purpose

Search the Mothdex catalog and put the right file in the right folder of the user's emulation system, safely: no
wrong file, no file outside the games folder, no surprise size. Success means the game shows up in the frontend after
the download, without the user touching a file manager.

## Positioning

Unofficial clients for the public Mothdex API, built for the device where the games are played: the GUI starts from
the frontend as a Port, and the profiles know each system's folders.

## Operating Context

- **Frontends:** EmulationStation-based frontends launch the GUI as a Port and get the screen and pad back on exit.
- **Downloads:** they come from the Mothdex proxy, which can take minutes to start ("warming up") and has no resume.
- **Languages:** en_us, pt_br, es_es and ru_ru.

## Capabilities and Constraints

- Search, platforms, regions, random, entry details with ranked links, a download queue, history, settings, covers.
- Gamepad only in the GUI; 640x480 to 1920x1080 plus a 480x320 scaled layout; SDL2 software renderer.
- No hardware of our own: the GUI is validated by community testers (D4).
- GPL-3.0. Fonts and assets must be redistributable and listed in `THIRD_PARTY`.

## Brand Commitments

- Name: Mothdex, written `mothdex` in the apps. The moth is the emblem.
- The GUI looks like mothdex.net, with a retro-game appearance (D29); the direction is "Night moth arcade" in
  `DESIGN.md` (D30). It takes inspiration only: no logo, artwork or files from the site or from any game.
- Voice: plain and factual, and states what happened and what to do next (cli-conventions, gui.md).

## Evidence on Hand

None to claim yet. There are no user numbers, testimonials or press, and no public page should invent them.

## Product Principles

1. Never surprise the user with a file: sizes, folders and consent are stated before anything is written.
2. Built for the device where the games are played: a pad, a TV, a small screen.
3. Facts over adjectives in every message.
4. Calm: nothing moves or blinks without a reason.

## Accessibility & Inclusion

WCAG 2.2 AA contrast for all text, a focus ring that passes 3:1, and no meaning carried by color alone. Text can be set
larger (`gui.text_size`). Every screen must stay usable at 640x480 in all four languages.
