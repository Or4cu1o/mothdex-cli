// Package config loads, validates and writes the Mothdex configuration.
//
// Precedence, per key: defaults (code) < YAML file < MOTHDEX_* environment < command-line
// flags. The schema is closed: one table (schema.go) drives defaults, validation, the
// environment mapping and the get/set/list/reset logic. CLI wiring lives elsewhere.
package config

import "time"

// SchemaVersion is the config schema version this build understands.
const SchemaVersion = 1

// Source says which layer supplied the effective value of a key.
type Source string

// Layers, lowest precedence first.
const (
	SourceDefault Source = "default"
	SourceFile    Source = "file"
	SourceEnv     Source = "env"
	SourceFlag    Source = "flag"
)

// Config is the typed, validated configuration. Load returns a fresh value on every call.
type Config struct {
	API       API
	Download  Download
	Cache     Cache
	Paths     Paths
	Interface Interface
	Profile   Profile
	Log       Log
	GUI       GUI
	Update    Update
}

// API holds the api-client settings.
type API struct {
	BaseURL    string
	Timeout    time.Duration
	RateLimit  float64 // req/s, clamped to 1..10
	MaxRetries int
}

// Download holds the downloader settings.
type Download struct {
	Dir              string // "" = active profile destination
	Concurrency      int
	MaxAttempts      int
	NotFoundRetries  int
	FirstByteTimeout time.Duration
	IdleTimeout      time.Duration
	OnExists         string // skip | rename | overwrite
	MaxFileSize      int64  // bytes
	Verify           bool
	Boxart           bool
	AllowedHosts     []string
	Prefer           Prefer
}

// Prefer holds the link ranking preferences.
type Prefer struct {
	Languages []string
	Formats   []string
	Sources   []string
	Size      string // none | smallest | largest
}

// Cache holds the cache settings.
type Cache struct {
	Enabled   bool
	MaxSizeMB int
	TTL       TTL
}

// TTL holds the per-namespace cache lifetimes.
type TTL struct {
	Search, Entry, Meta, Info, Covers time.Duration
}

// Paths holds the user-set directories; "" means the OS default (see ResolvePaths).
type Paths struct {
	Temp, Data, Logs string
}

// Interface holds the presentation settings.
type Interface struct {
	Language         string // auto | xx_yy (lower case)
	Color            string // auto | always | never
	ConfirmDownloads bool
	PageSize         int
}

// Profile holds the system-profile overrides.
type Profile struct {
	ID           string
	ROMRoot      string
	RetroBatPath string
	Folders      map[string]string // Mothdex platform id -> folder name
	Refresh      bool
}

// GUI holds the settings of the handheld front end (gui.md section 8, G2).
type GUI struct {
	ConfirmButton string // south | east
	TextSize      string // normal | large
	SafeArea      string // auto, or a percentage 0..10
}

// Log holds the logging settings.
type Log struct {
	Level     string
	File      bool
	Console   bool
	MaxSizeMB int
	MaxFiles  int

	SessionLevel string // level of the session file and latest.log
	SessionsKeep int    // session files kept
}

// Update configures release discovery and automatic checks.
type Update struct {
	Check   bool
	Channel string
}
