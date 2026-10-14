package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
	"github.com/vapour-computer/vapour-cli/internal/api"
	"github.com/vapour-computer/vapour-cli/internal/project"
	"github.com/vapour-computer/vapour-cli/internal/scaffold"
)

func newCreate() *cobra.Command {
	var engine, from string
	var offline bool
	cmd := &cobra.Command{
		Use:   "create <name>",
		Short: "Create a game project (scaffold, or clone a sample with --from)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			title := strings.TrimSpace(args[0])
			if title == "" {
				return fmt.Errorf("name is required")
			}
			if !validEngine(engine) {
				return fmt.Errorf("--engine must be one of %s", strings.Join(project.Engines, ", "))
			}
			slug := project.Slugify(title)
			dir := slug
			if entries, err := os.ReadDir(dir); err == nil && len(entries) > 0 {
				return fmt.Errorf("%s already exists and is not empty", dir)
			}

			// Register the project first, so nothing is left on disk if the server refuses it.
			m := &project.Manifest{
				Project: project.Project{Title: title, Slug: slug, Engine: engine, Version: "0.1.0"},
				Build:   project.Build{Dir: "build/web"},
			}
			if !offline {
				c, err := client(true)
				if err != nil {
					return err
				}
				g, err := c.CreateProject(cmd.Context(), api.CreateProjectRequest{Title: title, Slug: slug, Engine: engine})
				if err != nil {
					return fmt.Errorf("could not create the project: %w", err)
				}
				m.Project.ID, m.Project.Slug = g.ID, g.Slug
			}

			if from != "" {
				if err := clone(from, dir); err != nil {
					return err
				}
			} else if err := scaffold.Write(dir, engine, scaffold.Data{Title: title, Slug: m.Project.Slug, Engine: engine}); err != nil {
				return err
			}
			if err := m.Save(dir); err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			fmt.Fprintf(out, "Created %s in ./%s\n", title, dir)
			fmt.Fprintf(out, "Next: export your game for Web into %s/%s, then run `vapour publish` from ./%s\n", dir, m.Build.Dir, dir)
			return nil
		},
	}
	cmd.Flags().StringVar(&engine, "engine", "godot", "godot | unity | bevy | other")
	cmd.Flags().StringVar(&from, "from", "", "git URL of a sample game to clone into the new folder")
	cmd.Flags().BoolVar(&offline, "offline", false, "scaffold only; the project is registered on first publish")
	return cmd
}

func validEngine(e string) bool {
	for _, v := range project.Engines {
		if e == v {
			return true
		}
	}
	return false
}

// clone shallow-clones url into dir and drops the .git folder so the game starts with a fresh history.
func clone(url, dir string) error {
	if strings.HasPrefix(url, "-") {
		return fmt.Errorf("invalid git URL %q", url)
	}
	if _, err := exec.LookPath("git"); err != nil {
		return fmt.Errorf("git is required for --from")
	}
	c := exec.Command("git", "clone", "--depth", "1", "--", url, dir)
	c.Stderr = os.Stderr
	if err := c.Run(); err != nil {
		return fmt.Errorf("git clone failed: %w", err)
	}
	return os.RemoveAll(filepath.Join(dir, ".git"))
}
