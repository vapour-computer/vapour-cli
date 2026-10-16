package cmd

import (
	"github.com/spf13/cobra"
	"github.com/vapour-computer/vapour-cli/internal/enginebuild"
	"github.com/vapour-computer/vapour-cli/internal/project"
)

func newBuild() *cobra.Command {
	var dir string
	var noPerfHints bool
	c := &cobra.Command{Use: "build", Short: "Export a Godot, Unity or Bevy project for Web", Args: cobra.NoArgs, RunE: func(c *cobra.Command, _ []string) error {
		m, e := project.Load(dir)
		if e != nil {
			return e
		}
		if err := enginebuild.Build(c.Context(), dir, m, c.OutOrStdout()); err != nil {
			return err
		}
		emitPerfHints(c.OutOrStdout(), dir, m, noPerfHints)
		return nil
	}}
	c.Flags().StringVar(&dir, "dir", ".", "project directory containing vapour.toml")
	c.Flags().BoolVar(&noPerfHints, "no-perf-hints", false, "suppress performance hint warnings")
	return c
}
