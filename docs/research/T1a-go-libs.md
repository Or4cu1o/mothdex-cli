# T1a - Go libraries for Mothdex CLI

Researcher: Sonda. Date checked: 2026-11-20 (all rows). Method: `gh api` on upstream repos (releases, commits, go.mod, license API), go.dev/dl JSON.
Labels: Confirmed (primary source states it), Inferred (my reasoning), Unverified.

## Question
Which Go libraries should Mothdex CLI (GPL-3.0, `CGO_ENABLED=0`, linux amd64/arm64/armv7/386, windows, macOS) use for CLI, TUI, REPL, i18n, locale detection, config, rate limiting; what Go version floor follows?

## Short answer
- Go: build on **Go 1.27.4** (or 1.26.11, the other supported line). Nearly every current dep declares `go 1.26.0` in go.mod, so the floor is **go 1.26** in practice.
- Use **cobra v1.10.2**, **Charm v2** stack (`charm.land/...` import paths, not `github.com/charmbracelet/...`), **go-i18n v2.7.0**, **koanf v2.4.1**, **x/time/rate**.
- REPL: **peterh/liner** (live, tiny, MIT) or bubbletea textinput if TUI anyway; **ergochat/readline** is the fallback. Avoid chzyer/readline (no release since 2022) and reeflective/console (heavy deps, pulls cobra+carapace+mvdan/sh).
- Locale: **Xuanwo/go-locale** (pure Go at CGO=0 on all targets) or a stdlib-only ~40-line helper. Prefer the helper if only LANG/LC_* + Windows needed.
- All licenses found (MIT, BSD-3, Apache-2.0) are GPL-3.0 compatible (one-way: permissive code can be included in GPL-3.0 work). No copyleft or unknown-license transitive found in the Charm/cobra/koanf/i18n graphs examined.

## Evidence table

### Go toolchain
| Claim | Label | Source | Version | Date |
|---|---|---|---|---|
| Latest stable Go is go1.27.4; go1.26.11 also stable (supported lines) | Confirmed | https://go.dev/dl/?mode=json | 1.27.4 / 1.26.11 | 2026-11-20 |
| go1.27.4 tagged 2026-11-10 | Confirmed | `gh api repos/golang/go/commits/go1.27.4` | 1.27.4 | 2026-11-20 |
| Go 1.27 requires macOS 13 Ventura or later | Confirmed | https://go.dev/doc/go1.27 | 1.27 | 2026-11-20 |
| Go 1.27 release notes did not mention dropping linux/386, linux/arm (my grep found no such text) | Inferred | https://go.dev/doc/go1.27 | 1.27 | 2026-11-20 |
| GOARM=7 and GO386 builds remain available | Inferred (GoReleaser docs still list goarm/go386) | https://goreleaser.com/customization/builds/builders/go/ | v2.20.3 | 2026-11-20 |
| Go 1.27 GA date | Unverified (tag `go1.27` not resolved) | - | - | - |

### Libraries (release, maintenance, cgo, license)
| Lib | License (SPDX) | Latest tag / date | Last commit / issues | go.mod `go` | cgo? | Source |
|---|---|---|---|---|---|---|
| spf13/cobra | Apache-2.0 | v1.10.2 / 2025-12-04 | 2026-07-11; 513 open (large project) | 1.15 | No | https://github.com/spf13/cobra |
| charmbracelet/bubbletea | MIT | v2.1.2 / 2026-11-18 | 2026-11-20; 233 open | 1.26.0 | No | https://github.com/charmbracelet/bubbletea |
| charmbracelet/bubbles | MIT | v2.2.2 / 2026-11-20 | 2026-11-20; 198 open | 1.26.0 | No | https://github.com/charmbracelet/bubbles |
| charmbracelet/lipgloss | MIT | v2.1.1 / 2026-11-15 | 2026-11-18; 157 open | 1.26.0 | No | https://github.com/charmbracelet/lipgloss |
| ergochat/readline | MIT | v0.1.3 / 2024-09-09 | 2025-01-21; 13 open | 1.19 | No | https://github.com/ergochat/readline |
| reeflective/console | Apache-2.0 | v0.5.0 / 2026-07-18 | 2026-07-18; 1 open | 1.25.0 | No | https://github.com/reeflective/console |
| reeflective/readline | Apache-2.0 | v1.3.0 / 2026-07-05 | 2026-09-30 | n/a | No | https://github.com/reeflective/readline |
| chzyer/readline | MIT | v1.5.0 / 2022-04-14 | 2025-06-20; 132 open | n/a | No | https://github.com/chzyer/readline |
| peterh/liner | MIT | v1.2.3 (tag, no GH release) / commit 2026-10-08 | 2026-10-08; 17 open | 1.26.0 | No | https://github.com/peterh/liner |
| nicksnyder/go-i18n v2 | MIT | v2.7.0 / 2026-10-27 | 2026-11-01; 5 open | 1.26.0 | No | https://github.com/nicksnyder/go-i18n |
| Xuanwo/go-locale | Apache-2.0 | v1.1.3 / 2025-02-02 | 2026-07-04; 2 open | 1.20 | No (darwin shells out to `defaults`; windows reads registry `LocaleName`) | https://github.com/Xuanwo/go-locale |
| jeandeaual/go-locale | MIT | no tags/releases | 2025-06-12 (default branch HEAD) | 1.18 | Optional on darwin/ios/android (`*_cgo.go` with `*_nocgo.go` fallback) | https://github.com/jeandeaual/go-locale |
| knadh/koanf v2 | MIT | v2.4.1 / 2026-10-18 | 2026-11-19; 13 open | 1.23.0 | No | https://github.com/knadh/koanf |
| spf13/viper | MIT | v1.21.0 / 2025-09-08 | 2026-01-12 (default branch HEAD per pushed_at; last commit date seen 2025-10-15); 149 open | 1.23.0 | No | https://github.com/spf13/viper |
| golang.org/x/time | BSD-3-Clause | v0.16.0 / 2026-08-19 | 2026-09-08 | 1.26.0 | No | https://pkg.go.dev/golang.org/x/time/rate |

All dates/versions: gh API, checked 2026-11-20. "cgo: No" = Confirmed for what I could see: no `import "C"` found via code search for bubbletea, liner, ergochat/readline, Xuanwo/go-locale; jeandeaual has cgo only in optional files. Inferred for the rest from absence in dependency lists (pure Go deps: x/sys, x/text, ansi, uniseg, runewidth).

### Charm v2 status and import paths
| Claim | Label | Source | Version | Date |
|---|---|---|---|---|
| Bubble Tea v2 is GA (v2.0.0 on 2026-02-24; rc.1 2025-11-04) | Confirmed | https://github.com/charmbracelet/bubbletea/releases | v2.0.0 | 2026-11-20 |
| v2 module path is `charm.land/bubbletea/v2` | Confirmed (go.mod `module`) | https://github.com/charmbracelet/bubbletea/blob/v2.1.2/go.mod | v2.1.2 | 2026-11-20 |
| Bubbles v2: `charm.land/bubbles/v2`; Lip Gloss v2: `charm.land/lipgloss/v2` | Confirmed | go.mod of each repo | v2.2.2 / v2.1.1 | 2026-11-20 |
| Charm v2 libs require Go 1.26.0 | Confirmed | go.mod of each | as above | 2026-11-20 |
| Mixing v1 (`github.com/charmbracelet/...`) and v2 packages is not workable (v2 bubbles requires bubbletea/v2, lipgloss/v2) | Confirmed | bubbles v2.2.2 go.mod requires `charm.land/bubbletea/v2`, `charm.land/lipgloss/v2` | v2.2.2 | 2026-11-20 |
| Charm v2 graph pulls ultraviolet at a pseudo-version (`v0.0.0-2026...`), i.e. no tagged release | Confirmed | bubbletea go.mod | v2.1.2 | 2026-11-20 |

### Transitive licenses
| Claim | Label | Source | Date |
|---|---|---|---|
| Charm graph (colorprofile, ultraviolet, x/ansi, x/term, go-colorful, cancelreader, uniseg, runewidth, displaywidth, xo/terminfo, heredoc, harmonica, sahilm/fuzzy) all MIT per GitHub license API | Confirmed | `gh api repos/<r>` license.spdx_id | 2026-11-20 |
| atotto/clipboard (bubbles) BSD-3-Clause; go-udiff BSD-3-Clause | Confirmed | same | 2026-11-20 |
| dustin/go-humanize (bubbles) GitHub reports NOASSERTION; repository LICENSE is MIT-style | Inferred (not read in full) | https://github.com/dustin/go-humanize | 2026-11-20 |
| cobra deps: pflag BSD-3, mousetrap Apache-2.0, go-md2man (MIT, unchecked), yaml/v3 (MIT+Apache) | Confirmed (pflag, mousetrap) / Unverified (go-md2man) | go.mod + license API | 2026-11-20 |
| koanf core deps: go-viper/mapstructure MIT, mitchellh/copystructure MIT, koanf/maps MIT | Confirmed (first two) | license API | 2026-11-20 |
| go-i18n deps: BurntSushi/toml MIT, go.yaml.in/yaml/v3, x/text BSD-3 | Confirmed (toml) | license API | 2026-11-20 |
| reeflective/console pulls carapace (Apache-2.0), mvdan.cc/sh (BSD-3), reeflective/readline (Apache-2.0), kballard/go-shellquote (MIT), cobra | Confirmed | license API + go.mod | 2026-11-20 |
| Apache-2.0 is compatible with GPL-3.0 (not with GPL-2.0-only) | Confirmed | https://www.gnu.org/licenses/license-list.html#apache2 | 2026-11-20 |

## Options compared

### REPL / line editing
| Option | Pros | Cons |
|---|---|---|
| peterh/liner | Active (commit 2026-10-08), tiny deps (runewidth, x/sys), MIT, history+completion, Windows support | No vi mode; fewer features; no GH releases (tags only) |
| ergochat/readline | Rich (vi mode, undo, search), minimal deps, MIT, maintained fork of chzyer | Last commit 2025-01-21, last release 2024-09 (stale-ish but stable); v0.x API |
| chzyer/readline | Famous | Last release 2022-04, 132 open issues; superseded by ergochat fork |
| reeflective/console (+readline) | Full console with cobra integration, completions | Heavy graph (carapace, mvdan/sh), v0.x, small maintainer base, tied to cobra tree |
| bubbletea + bubbles textinput | Same stack as TUI, one dependency family | Not a readline: no history/completion out of the box, event-loop model |

### Config
| Option | Pros | Cons |
|---|---|---|
| knadh/koanf v2 | Modular (add only providers you use), small core deps, active (commit 2026-11-19), explicit precedence via load order | You wire cobra flags via posflag provider yourself |
| spf13/viper | De-facto with cobra, bindings built in | Large graph (afero, cast, fsnotify, locafero...), lowercases keys, slower cadence (v1.21.0 2025-09) |
| stdlib + small TOML/YAML decoder | Fewest deps | You write precedence logic |

### Locale detection
| Option | Pros | Cons |
|---|---|---|
| Xuanwo/go-locale | Pure Go on all targets; Linux env + Windows registry; returns x/text language tags | darwin execs `defaults` (fails in odd sandboxes); last release 2025-02 |
| jeandeaual/go-locale | MIT | No tagged releases; cgo optional on darwin (nocgo fallback) |
| stdlib helper | No dep; Linux order LC_ALL > LC_MESSAGES > LANG (strip `.UTF-8`, `@mod`); Windows `GetUserDefaultLocaleName` via `golang.org/x/sys/windows` (kernel32 LoadLazyDLL) | Must write and test ~40 lines; macOS needs `defaults read -g AppleLocale` or env |

## Recommendation
| Item | Pick | Condition |
|---|---|---|
| Go | go 1.27.4 in CI, `go 1.26.0` in go.mod (min) | Charm v2, go-i18n, x/time force >=1.26; if lower floor needed, drop Charm v2 / go-i18n 2.7 (older versions unverified) |
| CLI | cobra v1.10.2 | none |
| TUI | bubbletea/bubbles/lipgloss v2 via `charm.land/...` | Accept ultraviolet pseudo-version; pin via go.sum; start on v2 only |
| REPL | peterh/liner; fallback ergochat/readline | If vi mode needed choose ergochat; if TUI-only REPL, textinput |
| i18n | go-i18n v2.7.0 | Use `x/text/language` tags |
| Locale | stdlib helper or Xuanwo/go-locale v1.1.3 | Test on Linux/Windows/macOS CI runners |
| Config | koanf v2.4.1 | Document precedence flags > env > file > defaults |
| Rate limit | golang.org/x/time/rate (v0.16.0) | none |
| License | GPL-3.0 OK for all above | Ship THIRD_PARTY notices (MIT/BSD/Apache need copyright notices preserved); use `go-licenses` or similar in CI |

## Risks
- Charm v2 pulls `ultraviolet` and `x/exp` pseudo-versions: reproducibility depends on go.sum, no semver guarantees.
- 386/armv7 cross-compile with Charm v2 is Unverified here (pure Go, so expected to build; needs CI proof).
- peterh/liner is low-churn with a single maintainer; ergochat/readline similar, both low bus-factor.
- go.mod `go 1.26.0` blocks distro-packaged older Go toolchains for source builds (AUR source builds etc.).
- Apache-2.0 deps (cobra, mousetrap, Xuanwo/go-locale, reeflective) fine under GPL-3.0 but not GPL-2.0-only; if project is ever relicensed that matters.

## Open questions
1. Does Mothdex need vi-mode / rich completion in the REPL (decides liner vs ergochat)?
2. Is a lower Go floor than 1.26 required by distro packaging? If so, which older Charm/go-i18n versions satisfy it (not checked)?
3. Should rate limiting be client-side only (x/time/rate) or also honour server `Retry-After`?
4. Do we want a license-scan CI step (go-licenses) to enforce the compatibility list above?
