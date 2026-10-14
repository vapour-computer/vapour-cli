package cmd

import (
	"github.com/spf13/cobra"
	"github.com/vapour-computer/vapour-cli/internal/enginebuild"
	"github.com/vapour-computer/vapour-cli/internal/project"
)

func newBuild() *cobra.Command {
	var dir string
	c := &cobra.Command{Use: "build", Short: "Export a Godot, Unity or Bevy project for Web", Args: cobra.NoArgs, RunE: func(c *cobra.Command, _ []string) error {
		m, e := project.Load(dir)
		if e != nil {
			return e
		}
		return enginebuild.Build(c.Context(), dir, m, c.OutOrStdout())
	}}
	c.Flags().StringVar(&dir, "dir", ".", "project directory containing vapour.toml")
	return c
}
