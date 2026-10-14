package cmd

import (
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
	"github.com/vapour-computer/vapour-cli/internal/api"
	"github.com/vapour-computer/vapour-cli/internal/project"
	"github.com/vapour-computer/vapour-cli/internal/zipdir"
)

const maxBuild = 512 << 20 // matches the API limit

func newPublish() *cobra.Command {
	var dir, version string
	var dryRun bool
	cmd := &cobra.Command{
		Use:   "publish",
		Short: "Zip your web export from vapour.toml and upload it",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			m, err := project.Load(dir)
			if err != nil {
				return err
			}
			if version != "" {
				m.Project.Version = version
			}
			if err := m.Validate(); err != nil {
				return err
			}
			out := cmd.OutOrStdout()

			// Resolve the build: a folder is checked and zipped; a .zip is uploaded as is.
			buildPath := filepath.Join(dir, m.Build.Dir)
			zipPath, cleanup, err := prepareZip(buildPath)
			if err != nil {
				return err
			}
			defer cleanup()
			st, err := os.Stat(zipPath)
			if err != nil {
				return err
			}
			if st.Size() > maxBuild {
				return fmt.Errorf("build is %d MiB; the limit is %d MiB", st.Size()>>20, maxBuild>>20)
			}
			fmt.Fprintf(out, "Packed %s (%.1f MiB)\n", m.Build.Dir, float64(st.Size())/(1<<20))
			if dryRun {
				fmt.Fprintln(out, "Dry run: nothing uploaded.")
				return nil
			}

			c, err := client(true)
			if err != nil {
				return err
			}
			if m.Project.ID == "" { // created with --offline: register now
				g, err := c.CreateProject(cmd.Context(), api.CreateProjectRequest{Title: m.Project.Title, Slug: m.Project.Slug, Engine: m.Project.Engine})
				if err != nil {
					return fmt.Errorf("could not create the project: %w", err)
				}
				m.Project.ID, m.Project.Slug = g.ID, g.Slug
				if err := m.Save(dir); err != nil {
					return err
				}
			}

			f, err := os.Open(zipPath)
			if err != nil {
				return err
			}
			defer f.Close()
			var cover *api.File
			if m.Build.Cover != "" {
				cf, err := os.Open(filepath.Join(dir, m.Build.Cover))
				if err != nil {
					return fmt.Errorf("cover image: %w", err)
				}
				defer cf.Close()
				cover = &api.File{Name: m.Build.Cover, Body: cf}
			}
			fmt.Fprintf(out, "Uploading v%s to %s ...\n", m.Project.Version, c.Base)
			res, err := c.Publish(cmd.Context(), m.Project.ID, m.Project.Version, api.File{Name: "build.zip", Body: f}, cover)
			if err != nil {
				return err
			}
			fmt.Fprintf(out, "Published %s v%s\n%s\n", res.Game.Title, res.Game.Version, res.URL)
			return nil
		},
	}
	cmd.Flags().StringVar(&dir, "dir", ".", "game folder containing vapour.toml")
	cmd.Flags().StringVar(&version, "version", "", "override project.version for this publish")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "check and zip the build, but do not upload")
	return cmd
}

// prepareZip returns a path to a zip of buildPath and a cleanup function.
func prepareZip(buildPath string) (string, func(), error) {
	noop := func() {}
	st, err := os.Stat(buildPath)
	if err != nil {
		return "", noop, fmt.Errorf("build path %q not found: export your game for Web first", buildPath)
	}
	if !st.IsDir() {
		if !zipdir.IsZip(buildPath) {
			return "", noop, fmt.Errorf("%q is not a zip file", buildPath)
		}
		return buildPath, noop, nil
	}
	if err := zipdir.Check(buildPath); err != nil {
		return "", noop, err
	}
	tmp, err := os.CreateTemp("", "vapour-build-*.zip")
	if err != nil {
		return "", noop, err
	}
	cleanup := func() { os.Remove(tmp.Name()) }
	if _, err := zipdir.Zip(buildPath, io.Writer(tmp)); err != nil {
		tmp.Close()
		cleanup()
		return "", noop, err
	}
	if err := tmp.Close(); err != nil {
		cleanup()
		return "", noop, err
	}
	return tmp.Name(), cleanup, nil
}
