package library

import (
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/Or4cu1o/mothdex-cli/internal/config"
)

// Variables a profile path may use: ${HOME}, ${EXE_DIR}, ${CONFIG:<key>} and ${MATCH}, the
// directory (or glob match) found by the probe that selected the profile.
const (
	varHome   = "HOME"
	varExeDir = "EXE_DIR"
	varMatch  = "MATCH"
	varConfig = "CONFIG:"
)

// configKeys are the config values a profile may read.
var configKeys = map[string]bool{"profile.retrobat_path": true}

var tokenPattern = regexp.MustCompile(`\$\{([^}]*)\}`)

// checkVariables rejects unknown variables and stray dollar signs at load time.
func checkVariables(s string) error {
	for _, m := range tokenPattern.FindAllStringSubmatch(s, -1) {
		if err := checkToken(m[1]); err != nil {
			return err
		}
	}
	if strings.Contains(tokenPattern.ReplaceAllString(s, ""), "$") {
		return fmt.Errorf("path %q has a stray $", s)
	}
	return nil
}

func checkToken(name string) error {
	switch {
	case name == varHome, name == varExeDir, name == varMatch:
		return nil
	case strings.HasPrefix(name, varConfig) && configKeys[strings.TrimPrefix(name, varConfig)]:
		return nil
	default:
		return fmt.Errorf("unknown variable ${%s}", name)
	}
}

// values are what expansion substitutes.
type values struct {
	home, exeDir string
	match        string
	config       map[string]string
}

var errEmptyVariable = errors.New("variable expands to nothing")

// expand substitutes the variables of s; an empty value is an error, so that a missing home
// directory never turns "${HOME}/roms" into "/roms".
func (v values) expand(s string) (string, error) {
	var firstErr error
	out := tokenPattern.ReplaceAllStringFunc(s, func(tok string) string {
		name := tok[2 : len(tok)-1]
		val := v.lookup(name)
		if val == "" && firstErr == nil {
			firstErr = fmt.Errorf("${%s}: %w", name, errEmptyVariable)
		}
		return val
	})
	return out, firstErr
}

func (v values) lookup(name string) string {
	switch name {
	case varHome:
		return v.home
	case varExeDir:
		return v.exeDir
	case varMatch:
		return v.match
	default:
		return v.config[strings.TrimPrefix(name, varConfig)]
	}
}

// checkFolder applies CR-2 and CR-3 to a folder name: the shared plain-folder rule of the
// config package, plus the profile's own launcher folder names.
func checkFolder(folder string, launcher []string) error {
	if err := config.ValidateFolderName(folder); err != nil {
		return err
	}
	for _, l := range launcher {
		if strings.EqualFold(folder, l) || strings.EqualFold(strings.ToUpper(folder), strings.ToUpper(l)) {
			return fmt.Errorf("%q is a launcher folder and cannot receive downloads", folder)
		}
	}
	return nil
}
