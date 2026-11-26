//go:build gui

package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"github.com/Or4cu1o/mothdex-cli/internal/config"
	"github.com/Or4cu1o/mothdex-cli/internal/i18n"
	"github.com/Or4cu1o/mothdex-cli/locales"
)

const helpKey = "ui.gui.cmdline."

// helpFlags is the order of the flags in the help and the word that names the value of each ("" for a switch).
// The text of flag x-y is the message ui.gui.cmdline.flags.x_y.
var helpFlags = []struct{ name, value string }{
	{"config", "file"},
	{"profile", "id"},
	{"lang", "xx_yy"},
	{"log-level", "level"},
	{"window", "size"},
	{"driver", "name"},
	{"probe", ""},
	{"version", ""},
	{"spike", ""},
}

// helpExitCodes are the codes of cli-output.md section 4.1, in the order the help lists them.
var helpExitCodes = []int{exitOK, exitInternal, exitConfig, exitSDL}

// helpEnv are the variables the help names. MOTHDEX_<KEY> stands for the one of every configuration key.
var helpEnv = []struct{ name, key string }{
	{"MOTHDEX_CONFIG", "config"},
	{"MOTHDEX_LOG_LEVEL", "log_level"},
	{"MOTHDEX_<KEY>", "key"},
}

// runHelp is `mothdex-gui --help`: it loads no SDL2 and writes no file or log.
func runHelp(o options, stdout, stderr io.Writer) int {
	tr, err := cmdlineTranslator(o.config, o.lang)
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "mothdex-gui: %v\n", err)
		return exitInternal
	}
	writeHelp(stdout, tr)
	return exitOK
}

// usageError reports a command line the program cannot use, in the interface language, with the exit code of
// a configuration error.
func usageError(o options, stderr io.Writer, key string, args i18n.Args) int {
	tr, err := cmdlineTranslator(o.config, o.lang)
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "mothdex-gui: %v\n", err)
		return exitInternal
	}
	_, _ = fmt.Fprintf(stderr, "mothdex-gui: %s\n%s\n", tr.T(key, args), tr.T(helpKey+"see_help", nil))
	return exitConfig
}

// flagValue is the value of --name wherever it stands on the command line ("--name v" or "--name=v", one dash
// or two), for the error of a command line whose parsing stopped before reaching it. The last one wins.
func flagValue(args []string, name string) string {
	value := ""
	for i, a := range args {
		if a == "--" {
			break
		}
		if !strings.HasPrefix(a, "-") {
			continue
		}
		got, v, joined := strings.Cut(strings.TrimPrefix(strings.TrimPrefix(a, "-"), "-"), "=")
		switch {
		case got != name:
		case joined:
			value = v
		case i+1 < len(args) && !strings.HasPrefix(args[i+1], "-"):
			value = args[i+1]
		}
	}
	return value
}

// withLanguageFlags is o with --lang and --config taken from the whole command line: parsing stops at the first
// problem, and the two that pick the language of its message may come after it.
func withLanguageFlags(o options, args []string) options {
	o.lang, o.config = flagValue(args, "lang"), flagValue(args, "config")
	return o
}

// parseProblem names the message of a flag parsing error. The flag package only words its errors in English, so
// the two a person can meet are recognized by their text; a test pins it. Any other keeps that text.
func parseProblem(err error) (key string, args i18n.Args) {
	text := err.Error()
	for prefix, key := range map[string]string{
		"flag provided but not defined: ": "unknown_flag",
		"flag needs an argument: ":        "missing_value",
	} {
		if name, ok := strings.CutPrefix(text, prefix); ok {
			return helpKey + key, i18n.Args{"flag": name}
		}
	}
	return helpKey + "bad_flags", i18n.Args{"detail": text}
}

// cmdlineTranslator picks the language of the help and of the command line errors: --lang, else
// interface.language, else the system's. A configuration that does not load never hides the help.
func cmdlineTranslator(configFlag, lang string) (*i18n.Translator, error) {
	var file, localeDir string
	if plat, err := config.CurrentPlatform(); err == nil {
		if path, err := config.FilePath(plat, configFlag); err == nil {
			file, localeDir = path, filepath.Join(filepath.Dir(path), "locales")
		}
	}
	if lang == "" {
		lang = configuredLanguage(file)
	}
	bundle, err := i18n.LoadDir(locales.FS, localeDir)
	if err != nil {
		return nil, fmt.Errorf("cannot load the translations: %w", err)
	}
	return bundle.New(bundle.Resolve(lang, os.Getenv, i18n.SystemLanguages)), nil
}

// configuredLanguage is interface.language from the file and the environment, from the environment alone when
// the file does not load, and "auto" when neither does.
func configuredLanguage(file string) string {
	for _, path := range []string{file, ""} {
		if res, err := config.Load(config.Options{File: path, Environ: os.Environ()}); err == nil {
			return res.Config.Interface.Language
		}
	}
	return "auto"
}

func writeHelp(w io.Writer, tr *i18n.Translator) {
	text := func(key string) string { return tr.T(helpKey+key, nil) }
	section := func(title string, rows [][2]string) {
		_, _ = fmt.Fprintf(w, "\n%s\n", text(title))
		writeRows(w, rows)
	}

	_, _ = fmt.Fprintf(w, "%s\n\n%s\n", text("summary"), text("usage"))
	_, _ = fmt.Fprintf(w, "  mothdex-gui [%s]\n  mothdex-gui --probe | --version | --help\n  mothdex-gui --spike [--driver dummy]\n", text("options"))

	var rows [][2]string
	for _, f := range helpFlags {
		name := "--" + f.name
		switch f.value {
		case "":
		case "xx_yy": // the form of a locale id, the same in every language
			name += " <" + f.value + ">"
		default:
			name += " <" + text("values."+f.value) + ">"
		}
		rows = append(rows, [2]string{name, text("flags." + strings.ReplaceAll(f.name, "-", "_"))})
	}
	section("flags_title", append(rows, [2]string{"-h, --help", text("flags.help")}))
	_, _ = fmt.Fprintf(w, "\n%s\n", text("dashes"))

	rows = nil
	for _, code := range helpExitCodes {
		rows = append(rows, [2]string{fmt.Sprint(code), text(fmt.Sprintf("exit.c%d", code))})
	}
	section("exit_title", rows)

	rows = nil
	for _, v := range helpEnv {
		rows = append(rows, [2]string{v.name, text("env." + v.key)})
	}
	section("env_title", rows)

	_, _ = fmt.Fprintf(w, "\n%s\n  mothdex-gui --window 1280x720\n  mothdex-gui --lang pt_br --log-level debug\n  mothdex-gui --probe\n", text("examples_title"))
	_, _ = fmt.Fprintf(w, "\n%s\n", text("log_note"))
}

// writeRows writes two aligned columns. A line break in a message continues in the second column.
func writeRows(w io.Writer, rows [][2]string) {
	width := 0
	for _, r := range rows {
		width = max(width, utf8.RuneCountInString(r[0]))
	}
	indent := "\n" + strings.Repeat(" ", width+4)
	for _, r := range rows {
		pad := strings.Repeat(" ", width-utf8.RuneCountInString(r[0]))
		_, _ = fmt.Fprintf(w, "  %s%s  %s\n", r[0], pad, strings.ReplaceAll(r[1], "\n", indent))
	}
}
