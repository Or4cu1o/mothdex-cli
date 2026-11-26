package cli

import (
	"bytes"
	"fmt"
	"regexp"
	"strings"

	"github.com/spf13/cobra"
)

func (r *runtime) completionCmd() *cobra.Command {
	c := &cobra.Command{Use: "completion", Short: "Generate shell completion for mothdex and mdex"}
	for _, shell := range []string{"bash", "zsh", "fish", "powershell"} {
		sub := &cobra.Command{Use: shell, Short: "Generate " + shell + " completion", Args: cobra.NoArgs}
		sub.Long = "Generate " + shell + " completion for mothdex and mdex. Load the script in your shell startup file."
		sub.Example = "mothdex completion " + shell
		sub.RunE = func(cmd *cobra.Command, _ []string) error { return writeCompletion(cmd, shell) }
		c.AddCommand(sub)
	}
	return c
}

func writeCompletion(cmd *cobra.Command, shell string) error {
	// Generated function names stay canonical even when invoked as mdex.
	root := cmd.Root()
	use := root.Use
	aliases := []string{"mdex", "mthd"}
	if valid, _ := regexp.MatchString(`^[a-z][a-z0-9-]{1,15}$`, root.Name()); valid && root.Name() != "mothdex" && root.Name() != "mdex" && root.Name() != "mthd" {
		aliases = append(aliases, root.Name())
	}
	root.Use = "mothdex"
	defer func() { root.Use = use }()
	var b bytes.Buffer
	var err error
	switch shell {
	case "bash":
		err = root.GenBashCompletionV2(&b, true)
	case "zsh":
		err = root.GenZshCompletion(&b)
	case "fish":
		err = root.GenFishCompletion(&b, true)
	case "powershell":
		err = root.GenPowerShellCompletionWithDesc(&b)
	}
	if err != nil {
		return err
	}
	script := b.String()

	for _, alias := range aliases {
		switch shell {
		case "bash":
			script += "\ncomplete -o default -F __start_mothdex " + alias + "\n"
		case "zsh":
			script = strings.Replace(script, "#compdef mothdex", "#compdef mothdex "+alias, 1)
			script += "\ncompdef _mothdex " + alias + "\n"
		case "fish":
			script += "\ncomplete -c " + alias + " -w mothdex\n"
		}
	}
	if shell == "powershell" {
		script = strings.ReplaceAll(script, "-CommandName 'mothdex'", "-CommandName mothdex,"+strings.Join(aliases, ","))
	}
	_, err = fmt.Fprint(cmd.OutOrStdout(), script)
	return err
}
