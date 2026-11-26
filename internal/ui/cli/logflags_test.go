package cli

import (
	"strings"
	"testing"
)

// The session header shows only what is annotated: paths and addresses must never be (TM-17).
func TestPathFlagsAreNeverLogged(t *testing.T) {
	rules := LogFlagRules()
	for _, name := range []string{"config", "output", "o", "export", "e"} {
		if r, ok := rules[name]; !ok || r.Log || !r.TakesValue {
			t.Errorf("%s = %+v, %v", name, r, ok)
		}
	}
	for _, name := range []string{"json", "platform", "p", "link", "lang"} {
		if !rules[name].Log {
			t.Errorf("%s is not loggable", name)
		}
	}
}

func TestOnlyRealRunsGetASessionLog(t *testing.T) {
	for line, want := range map[string]bool{
		"":                       true,
		"search mario":           true,
		"--lang pt_br search x":  true,
		"search version":         true,
		"logs path":              false,
		"--json logs bundle":     false,
		"__complete search x":    false,
		"__completeNoDesc x":     false,
		"completion bash":        false,
		"--lang pt_br version":   false,
		"--version":              false,
		"--config x.yaml shell":  true,
		"--json --verbose cache": true,
	} {
		if got := Logged(strings.Fields(line), LogFlagRules()); got != want {
			t.Errorf("Logged(%q) = %v", line, got)
		}
	}
}
