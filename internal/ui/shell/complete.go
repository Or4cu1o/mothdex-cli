package shell

import (
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/Or4cu1o/mothdex-cli/internal/ui/cli"
)

// metaCommands are the Shell's own commands, besides the ones of the CLI.
var metaCommands = []string{"clear", "exit", "help", "last", "n", "p", "page", "quit", "sort"}

var (
	sortKeys      = []string{"relevance", "title", "size"}
	exportFormats = []string{"txt", "json", "csv"}
	preferKeys    = []string{"lang=", "format=", "source=", "size="}
	sizeValues    = []string{"smallest", "largest"}
	// configEnums are the values of the settings that take a fixed set. Static until the app
	// layer exposes the config schema (ui.md section 5, gap F1).
	configEnums = map[string][]string{
		"download.on_exists": {"skip", "rename", "overwrite"},
		"interface.color":    {"auto", "always", "never"},
	}
)

// maxPathCandidates bounds the file names offered for one Tab.
const maxPathCandidates = 200

// completionSources are the lists Tab draws from. Each may be nil, and returns nothing when its
// source fails: completion never prints an error while the user types (shell.md section 6).
type completionSources struct {
	commands   []cli.CommandInfo
	platforms  func() []string
	regions    func() []string
	configKeys func() []string
	cacheNS    func() []string
	entryLinks func() (langs, formats, sources []string)
	pageLen    func() int
	paths      func(prefix string) []string
}

func list(f func() []string) []string {
	if f == nil {
		return nil
	}
	return f()
}

// complete is the word completer: the line, the cursor in runes, and the answer is the text before
// the word, the candidates that replace it, and the text after the cursor.
func complete(line string, pos int, src completionSources) (head string, cands []string, tail string) {
	runes := []rune(line)
	pos = max(0, min(pos, len(runes)))
	before, tail := string(runes[:pos]), string(runes[pos:])
	start := strings.LastIndexAny(before, " \t") + 1
	head, word := before[:start], before[start:]
	return head, candidates(strings.Fields(head), word, src), tail
}

// candidates answers by position: the tokens before the word, and the word being completed.
func candidates(tokens []string, word string, src completionSources) []string {
	if len(tokens) == 0 {
		names := append([]string{}, metaCommands...)
		for _, c := range src.commands {
			names = append(names, c.Name)
		}
		return finish(names, word, false)
	}
	name := tokens[0]
	if name == "d" {
		name = "download"
	}
	cmd, known := findCommand(src.commands, name)
	if strings.HasPrefix(word, "-") {
		return finish(flagWords(cmd), word, false)
	}
	if len(tokens) > 1 && strings.HasPrefix(tokens[len(tokens)-1], "-") {
		if flag, ok := findFlag(cmd, tokens[len(tokens)-1]); ok && flag.TakesValue {
			return flagValues(flag.Name, word, src)
		}
	}
	switch {
	case name == "help":
		return finish(append(commandNames(src.commands), metaCommands...), word, false)
	case name == "sort":
		return finish(sortKeys, word, false)
	case !known:
		return nil
	case len(cmd.Subs) > 0:
		return subArgs(cmd, tokens[1:], word, src)
	case name == "info" || name == "download" || name == "boxart":
		return finish(pageNumbers(src), word, true)
	}
	return nil
}

func commandNames(cmds []cli.CommandInfo) []string {
	out := make([]string, len(cmds))
	for i, c := range cmds {
		out[i] = c.Name
	}
	return out
}

func findCommand(cmds []cli.CommandInfo, name string) (cli.CommandInfo, bool) {
	for _, c := range cmds {
		if c.Name == name {
			return c, true
		}
	}
	return cli.CommandInfo{}, false
}

// findFlag finds the flag a word such as `-p` or `--platform` names.
func findFlag(cmd cli.CommandInfo, word string) (cli.FlagInfo, bool) {
	for _, f := range cmd.Flags {
		if word == "--"+f.Name || (f.Short != "" && word == "-"+f.Short) {
			return f, true
		}
	}
	return cli.FlagInfo{}, false
}

func flagWords(cmd cli.CommandInfo) []string {
	var out []string
	for _, f := range cmd.Flags {
		out = append(out, "--"+f.Name)
		if f.Short != "" {
			out = append(out, "-"+f.Short)
		}
	}
	return out
}

func pageNumbers(src completionSources) []string {
	if src.pageLen == nil {
		return nil
	}
	out := make([]string, src.pageLen())
	for i := range out {
		out[i] = strconv.Itoa(i + 1)
	}
	return out
}

// flagValues completes the value of a flag by its long name.
func flagValues(flag, word string, src completionSources) []string {
	switch flag {
	case "platform":
		return finish(list(src.platforms), word, false)
	case "region":
		return finish(list(src.regions), word, false)
	case "sort":
		return finish(sortKeys, word, false)
	case "format":
		return finish(exportFormats, word, false)
	case "export":
		if src.paths == nil {
			return nil
		}
		return finish(src.paths(word), word, false)
	case "prefer":
		return preferValues(word, src)
	}
	return nil
}

// preferValues completes `key=value` (prefer-syntax.md): the keys first, then the values of a key.
// Values of lang, format and source come from the links of the last entry shown, when there is one.
func preferValues(word string, src completionSources) []string {
	key, val, hasValue := strings.Cut(word, "=")
	if !hasValue {
		return finish(preferKeys, word, false)
	}
	var values []string
	switch strings.ToLower(key) {
	case "size":
		values = sizeValues
	case "lang", "format", "source":
		if src.entryLinks == nil {
			return nil
		}
		langs, formats, sources := src.entryLinks()
		values = map[string][]string{"lang": langs, "format": formats, "source": sources}[strings.ToLower(key)]
	default:
		return nil
	}
	out := make([]string, 0, len(values))
	for _, v := range finish(values, val, false) {
		out = append(out, key+"="+v)
	}
	return out
}

// subArgs completes inside commands that have subcommands: config get|set|reset <key> [value] and
// cache clear [namespace...].
func subArgs(cmd cli.CommandInfo, rest []string, word string, src completionSources) []string {
	if len(rest) == 0 {
		return finish(commandNames(cmd.Subs), word, false)
	}
	sub, args := rest[0], rest[1:]
	switch {
	case cmd.Name == "config" && (sub == "get" || sub == "set" || sub == "reset"):
		switch {
		case len(args) == 0 && sub == "reset":
			return finish(append(list(src.configKeys), "all"), word, false)
		case len(args) == 0:
			return finish(list(src.configKeys), word, false)
		case len(args) == 1 && sub == "set":
			return finish(configEnums[args[0]], word, false)
		}
	case cmd.Name == "cache" && sub == "clear":
		return finish(list(src.cacheNS), word, false)
	}
	return nil
}

// finish keeps the candidates that start with the word (any case), drops duplicates and sorts
// them case-insensitively; numbers keep their given order.
func finish(all []string, word string, keepOrder bool) []string {
	lower := strings.ToLower(word)
	seen := map[string]bool{}
	var out []string
	for _, c := range all {
		if !strings.HasPrefix(strings.ToLower(c), lower) || seen[c] {
			continue
		}
		seen[c] = true
		out = append(out, c)
	}
	if !keepOrder {
		sort.Slice(out, func(i, j int) bool { return strings.ToLower(out[i]) < strings.ToLower(out[j]) })
	}
	return out
}

// completePaths lists the files and folders that start with prefix, folders with a trailing
// separator. It is the file source of `-e/--export`.
func completePaths(prefix string) []string {
	dir, base := filepath.Split(prefix)
	scan := dir
	if scan == "" {
		scan = "."
	}
	entries, err := os.ReadDir(scan)
	if err != nil {
		return nil
	}
	var out []string
	for _, e := range entries {
		if !strings.HasPrefix(e.Name(), base) {
			continue
		}
		name := dir + e.Name()
		if e.IsDir() {
			name += string(filepath.Separator)
		}
		if out = append(out, name); len(out) >= maxPathCandidates {
			break
		}
	}
	return out
}
