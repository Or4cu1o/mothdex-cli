//go:build !windows

package i18n

// SystemLanguages has nothing beyond the POSIX variables off Windows.
func SystemLanguages() []string { return nil }
