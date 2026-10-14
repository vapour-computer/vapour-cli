package cmd

import (
	"context"
	"errors"
	"fmt"
	"io"
	"path/filepath"

	"github.com/spf13/cobra"
	"github.com/vapour-computer/vapour-cli/internal/api"
	"github.com/vapour-computer/vapour-cli/internal/project"
	"github.com/vapour-computer/vapour-cli/internal/storepage"
)

func newStore() *cobra.Command {
	root := &cobra.Command{
		Use:   "store",
		Short: "Check or update your game's store page (text, images, banner, trailer)",
	}
	var dir string
	check := &cobra.Command{
		Use:   "check",
		Short: "Validate store/page.json, store/template.md and the media files without uploading",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			m, err := project.Load(dir)
			if err != nil {
				return err
			}
			b, err := storepage.Load(dir, m)
			if err != nil {
				return err
			}
			if b == nil {
				fmt.Fprintln(cmd.OutOrStdout(), "No store page configured. Add a [store] section to vapour.toml (vapour create writes one).")
				return nil
			}
			printBundle(cmd.OutOrStdout(), b)
			fmt.Fprintln(cmd.OutOrStdout(), "Store page looks good.")
			return nil
		},
	}
	push := &cobra.Command{
		Use:   "push",
		Short: "Upload the store page without publishing a new build",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			m, err := project.Load(dir)
			if err != nil {
				return err
			}
			b, err := storepage.Load(dir, m)
			if err != nil {
				return err
			}
			if b == nil {
				return errors.New("no store page configured: add a [store] section to vapour.toml")
			}
			c, err := client(true)
			if err != nil {
				return err
			}
			if err := ensureProject(cmd.Context(), c, m, dir); err != nil {
				return err
			}
			printBundle(cmd.OutOrStdout(), b)
			return pushStore(cmd.Context(), cmd.OutOrStdout(), c, m, b)
		},
	}
	for _, c := range []*cobra.Command{check, push} {
		c.Flags().StringVar(&dir, "dir", ".", "game folder containing vapour.toml")
		root.AddCommand(c)
	}
	return root
}

// ensureProject registers the project on first use (a project made with `vapour create --offline` has no id yet).
func ensureProject(ctx context.Context, c *api.Client, m *project.Manifest, dir string) error {
	if m.Project.ID != "" {
		return nil
	}
	g, err := c.CreateProject(ctx, api.CreateProjectRequest{Title: m.Project.Title, Slug: m.Project.Slug, Engine: m.Project.Engine})
	if err != nil {
		return fmt.Errorf("could not create the project: %w", err)
	}
	m.Project.ID, m.Project.Slug = g.ID, g.Slug
	return m.Save(dir)
}

func printBundle(out io.Writer, b *storepage.Bundle) {
	if b == nil {
		return
	}
	if d, ok := b.Metadata["description"].(string); ok {
		fmt.Fprintf(out, "Store text: %d characters\n", len([]rune(d)))
	}
	if c, ok := b.Metadata["category"].(string); ok {
		fmt.Fprintf(out, "Category: %s\n", c)
	}
	for _, u := range b.Uploads {
		fmt.Fprintf(out, "  %-10s %s (%.1f MiB)\n", u.Field, filepath.Base(u.Path), float64(u.Bytes)/(1<<20))
	}
	if b.Notes != "" {
		fmt.Fprintf(out, "Release notes: %d characters\n", len([]rune(b.Notes)))
	}
	for _, w := range b.Warnings {
		fmt.Fprintf(out, "warning: %s\n", w)
	}
}

func pushStore(ctx context.Context, out io.Writer, c *api.Client, m *project.Manifest, b *storepage.Bundle) error {
	parts := make([]api.Part, 0, len(b.Uploads))
	for _, u := range b.Uploads {
		parts = append(parts, api.Part{Field: u.Field, Path: u.Path})
	}
	fmt.Fprintf(out, "Updating the store page (%.1f MiB of media) ...\n", float64(b.TotalBytes())/(1<<20))
	if _, err := c.PutStore(ctx, m.Project.ID, b.Metadata, parts); err != nil {
		return fmt.Errorf("could not update the store page: %w", err)
	}
	fmt.Fprintln(out, "Store page updated.")
	return nil
}
