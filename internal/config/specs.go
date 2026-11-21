package config

// Kinds of a setting, for UIs that edit or complete values.
const (
	KindString   = "string"
	KindEnum     = "enum"
	KindInt      = "int"
	KindFloat    = "float"
	KindBool     = "bool"
	KindDuration = "duration" // "30s", "10m"
	KindSize     = "size"     // "256GiB"
	KindList     = "list"     // comma-separated
)

// SettingSpec describes one schema key: what a UI needs for the Settings screen and for completion.
// Min and Max are written the way a user types them (a count, a duration, a size); empty when the
// kind has no range. Env is the environment variable that overrides the key.
type SettingSpec struct {
	Key, Kind string
	Enum      []string
	Min, Max  string
	Env       string
}

// Specs lists every schema key in `config list` order. The slices are copies.
func Specs() []SettingSpec {
	out := make([]SettingSpec, len(schema))
	for i, s := range schema {
		out[i] = s.spec()
	}
	return out
}
