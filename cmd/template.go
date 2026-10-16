package cmd

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"
)

const defaultTemplateOrg = "vapour-computer"

var templateRepos = map[string]string{
	"bevy":  "vapour-template-bevy",
	"godot": "vapour-template-godot",
	"unity": "vapour-template-unity",
}

func newTemplate() *cobra.Command {
	var org string
	var offline bool
	var allowUntrustedTemplate bool
	cmd := &cobra.Command{
		Use:   "template <engine> <name>",
		Short: "Create a project from an official template repo",
		Long:  "Clone an engine template from your Git org, remove .git, then initialize vapour.toml and store files.",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			engine := strings.ToLower(strings.TrimSpace(args[0]))
			title := strings.TrimSpace(args[1])
			from, err := templateRepoURL(org, engine)
			if err != nil {
				return err
			}
			return createProject(cmd, title, engine, "desktop", from, offline, allowUntrustedTemplate)
		},
	}
	cmd.Flags().StringVar(&org, "org", defaultTemplateOrg, "GitHub org/user that owns template repos")
	cmd.Flags().BoolVar(&offline, "offline", false, "scaffold only; the project is registered on first publish")
	cmd.Flags().BoolVar(&allowUntrustedTemplate, "allow-untrusted-template", false, "allow cloning templates outside trusted sources")
	return cmd
}

func templateRepoURL(org, engine string) (string, error) {
	name, ok := templateRepos[engine]
	if !ok {
		return "", fmt.Errorf("template engine must be one of bevy, godot, unity")
	}
	if strings.TrimSpace(org) == "" {
		return "", fmt.Errorf("--org is required")
	}
	return fmt.Sprintf("https://github.com/%s/%s.git", strings.TrimSpace(org), name), nil
}
