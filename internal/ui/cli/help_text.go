package cli

// helpText keeps descriptions and runnable examples together. Commands always use mothdex in
// examples, even when Usage reflects an installed alias.
type helpText struct{ long, example string }

var commandHelp = map[string]helpText{
	"root":           {"Search the catalog, inspect entries and download games. Use --json for scripts.\nRun mothdex help config, mothdex help exit-codes or mothdex help errors for reference.", "mothdex search mario\nmothdex help config"},
	"search":         {"Search titles and save a numbered result page for info and download. Filter by platform or region; sorting applies to the current page.", "mothdex search mario\nmothdex search mario --platform snes --page-size 10"},
	"info":           {"Show an entry and its download links. Use a number from the last search or an explicit slug with --slug.", "mothdex search mario\nmothdex info 1"},
	"random":         {"Pick random catalog entries, optionally filtered by platform and region.", "mothdex random"},
	"platforms":      {"List platform identifiers accepted by --platform.", "mothdex platforms"},
	"regions":        {"List region identifiers accepted by --region.", "mothdex regions"},
	"db_info":        {"Show catalog totals and database metadata from the server.", "mothdex db-info"},
	"download":       {"Download by result number or slug. Run search first when using numbers. --search selects the first match. --link selects a numbered link; -o chooses a destination folder and --boxart saves its cover.", "mothdex search mario\nmothdex download 1\nmothdex download 1 -o ./games\nmothdex download --search mario\nmothdex download 1 --link 1\nmothdex download 1 --boxart"},
	"boxart":         {"Save an entry cover to the destination folder without downloading the game.", "mothdex search mario\nmothdex boxart 1 -o ./games"},
	"history":        {"List previous downloads, including failures. Export records using --export and --format.", "mothdex history\nmothdex history --failed"},
	"cache":          {"Inspect local cache usage or clear cached catalog data. Session and state are preserved by default.", "mothdex cache stats"},
	"cache_stats":    {"Show item counts, sizes and expiry by cache namespace.", "mothdex cache stats"},
	"cache_clear":    {"Clear selected namespaces, or cached data other than session and state when none are given.", "mothdex cache clear"},
	"config":         {"Read effective settings and their sources, or edit the YAML configuration. Run mothdex help config for accepted values, defaults and environment overrides.", "mothdex config list"},
	"config_list":    {"List effective values and their sources: default, file, environment or command flag.", "mothdex config list"},
	"config_get":     {"Print the effective value of one dotted configuration key.", "mothdex config get interface.language"},
	"config_set":     {"Validate and save a configuration value. The next invocation reads the new value.", "mothdex config set interface.page_size 10"},
	"config_reset":   {"Remove an override so a key, section or the whole file returns to defaults.", "mothdex config reset interface.page_size"},
	"profile":        {"Inspect the active frontend profile and detected game folders.", "mothdex profile show"},
	"profile_show":   {"Show the active frontend, game root, detection source and storage warnings.", "mothdex profile show"},
	"profile_detect": {"Detect the frontend profile again and show the resulting folders and warnings.", "mothdex profile detect"},
	"shell":          {"Open an interactive session with history and Tab completion. Shell-only commands: n, p, page, sort, last, clear, exit and quit.", "mothdex shell"},
	"tui":            {"Open the full-screen terminal interface. Both input and output must be interactive terminals.", "mothdex tui"},
	"gui":            {"Run mothdex-gui from the CLI installation folder. Forward --config, --profile and --lang to the GUI. Reinstall without -NoGui / --no-gui if it is missing, or use mothdex tui.", "mothdex gui\nmothdex gui --lang pt_br"},
	"version":        {"Print version, commit and build date. --json returns the standard output envelope.", "mothdex version"},
	"help":           {"Show help for a command or a reference topic: exit-codes, config or errors.", "mothdex help download\nmothdex help exit-codes\nmothdex help config\nmothdex help errors"},
	"completion":     {"Generate a shell completion script for mothdex and mdex. Load it with source (bash/zsh), pipe to source (fish), or Invoke-Expression (PowerShell).", "mothdex completion bash"},
}

const modesHelp = `Mode    Open                         Purpose
CLI     mdex <command>               Scripts and quick use; accepts --json
Shell   mdex, or mdex shell          Sequential commands, n/p, history, Tab
TUI     mdex tui                     Full-screen keyboard navigation
GUI     mdex gui, or mothdex-gui     Gamepad interface for RetroBat, Batocera and similar systems`
