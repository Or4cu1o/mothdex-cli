package config

import (
	"fmt"
	"strings"
)

const (
	envPrefix = "MOTHDEX_"
	// EnvConfig is the reserved variable that selects the config file.
	EnvConfig = "MOTHDEX_CONFIG"
)

// envName maps a key to its variable: MOTHDEX_ + key in upper case, dots as underscores.
func envName(key string) string {
	return envPrefix + strings.ToUpper(strings.ReplaceAll(key, ".", "_"))
}

// envKeys is the fixed variable -> key table generated from the schema. A test asserts
// that no two keys share a variable.
var envKeys = func() map[string]string {
	m := make(map[string]string, len(schema))
	for _, s := range schema {
		if !s.noEnv() {
			m[envName(s.key())] = s.key()
		}
	}
	return m
}()

// envLayer extracts the MOTHDEX_* variables from an environ slice (KEY=VALUE entries).
// foldCase upper-cases names first (Windows variable names ignore case). Unknown MOTHDEX_* variables become warnings; MOTHDEX_CONFIG is reserved and skipped.
func envLayer(environ []string, foldCase bool) (values map[string]any, warnings []string) {
	values = map[string]any{}
	for _, kv := range environ {
		name, val, ok := strings.Cut(kv, "=")
		if foldCase {
			name = strings.ToUpper(name)
		}
		if !ok || !strings.HasPrefix(name, envPrefix) || (name == EnvConfig || name == "MOTHDEX_NO_UPDATE_CHECK") {
			continue
		}
		key, known := envKeys[name]
		if !known {
			warnings = append(warnings, fmt.Sprintf("unknown environment variable %s ignored", name))
			continue
		}
		values[key] = val
	}
	return values, warnings
}
