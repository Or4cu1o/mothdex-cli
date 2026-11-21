// Package profiles embeds the system profiles (docs/contracts/cli/profiles.md). They are
// data: internal/library parses and validates them, and CI validates every file.
package profiles

import "embed"

// FS holds the embedded profile files, one YAML file per profile id.
//
//go:embed *.yaml
var FS embed.FS
