// Package locales embeds the translation files (one YAML file per locale, named xx_yy.yaml).
package locales

import "embed"

// FS holds the built-in locales. en_us is the source of truth: every other locale falls back to
// it key by key.
//
//go:embed *.yaml
var FS embed.FS
