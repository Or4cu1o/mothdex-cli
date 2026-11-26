package cli

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"github.com/Or4cu1o/mothdex-cli/internal/i18n"
	"github.com/Or4cu1o/mothdex-cli/locales"
)

func TestEveryCommandAndFlagHasEnglishHelp(t *testing.T) {
	b, err := i18n.Load(locales.FS)
	if err != nil {
		t.Fatal(err)
	}
	source := b.Catalog("en_us")
	d, _ := helpDeps(t)
	trees := []*cobra.Command{(&runtime{d: d}).rootCommand(), (&runtime{d: d, run: NewRunner(d, &Session{T: b.New("en_us")}, false)}).rootCommand()}
	for _, root := range trees {
		walkCommands(root, func(c *cobra.Command) {
			if c.Hidden {
				return
			}
			for _, part := range []string{"short", "long", "example"} {
				key := "ui.cli.help." + helpID(c) + "." + part
				if strings.TrimSpace(source[key]) == "" {
					t.Errorf("%s lacks %s", c.CommandPath(), key)
				}
			}
			check := func(f *pflag.Flag) {
				if f.Hidden {
					return
				}
				key := "ui.cli.flags." + strings.ReplaceAll(f.Name, "-", "_")
				if strings.TrimSpace(source[key]) == "" {
					t.Errorf("%s --%s lacks %s", c.CommandPath(), f.Name, key)
				}
			}
			c.Flags().VisitAll(check)
			c.PersistentFlags().VisitAll(check)
			c.InheritedFlags().VisitAll(check)
		})
	}
}

func TestHelpLanguagePrecedenceAndFallback(t *testing.T) {
	// Explicit file keeps this test independent of the machine's own configuration.
	file := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(file, []byte("interface:\n  language: pt_br\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name string
		args []string
		want []string
	}{
		{"configured Portuguese", []string{"--help"}, []string{"Uso:", "Catálogo:", "Sistema:", "Flags globais:", "Modos de uso:", "Pesquise no catálogo"}},
		{"explicit Portuguese", []string{"--lang", "pt_br", "download", "--help"}, []string{"Uso:", "Exemplos:", "Baixe pelo número", "número do link", "Flags globais:"}},
		{"explicit English beats file", []string{"--lang=en_us", "download", "--help"}, []string{"Usage:", "Examples:", "Download by result number", "link number", "Global Flags:"}},
		{"Spanish help falls back", []string{"--lang=es_es", "--help"}, []string{"Usage:", "Catalog:", "Global Flags:", "Modes:"}},
		{"unknown locale falls back", []string{"--lang=zz_zz", "--help"}, []string{"Usage:", "Catalog:"}},
		{"Portuguese config topic", []string{"--lang=pt_br", "help", "config"}, []string{"Configuração:", "Chave | Valores aceitos | Padrão | Ambiente"}},
		{"Portuguese error topic", []string{"--lang=pt_br", "help", "errors"}, []string{"Referência de erros", "Como corrigir", "Nenhuma ação necessária"}},
		{"Portuguese exit topic", []string{"--lang=pt_br", "help", "exit-codes"}, []string{"Saída Significado", "Atualização não aplicada"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d, out := helpDeps(t)
			d.ProgramName = "mdex"
			d.Open = func(context.Context, Globals) (*Session, error) { t.Fatal("help opened the service"); return nil, nil }
			args := append([]string{"--config", file}, tc.args...)
			if code := Execute(context.Background(), d, args); code != 0 {
				t.Fatalf("exit %d: %s", code, out)
			}
			for _, want := range tc.want {
				if !strings.Contains(out.String(), want) {
					t.Errorf("missing %q in %s", want, out)
				}
			}
			if strings.Contains(out.String(), "ui.cli.") {
				t.Errorf("unresolved key: %s", out)
			}
		})
	}
}

func TestShellCommandDescriptionsFollowSessionLanguage(t *testing.T) {
	b, err := i18n.Load(locales.FS)
	if err != nil {
		t.Fatal(err)
	}
	d, _ := helpDeps(t)
	runner := NewRunner(d, &Session{T: b.New("pt_br")}, false)
	for _, c := range runner.Commands() {
		if c.Name == "search" && c.Short != "Pesquise no catálogo" {
			t.Fatalf("shell search description: %s", c.Short)
		}
	}
}
