package cmd

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"unicode/utf8"

	"github.com/spf13/cobra"
	"github.com/vapour-computer/vapour-cli/internal/api"
	"github.com/vapour-computer/vapour-cli/internal/project"
	"github.com/vapour-computer/vapour-cli/internal/storepage"
	"github.com/vapour-computer/vapour-cli/internal/zipdir"
)

const maxBuild = 512 << 20 // matches the API limit

func newPublish() *cobra.Command {
	var dir, version, notes, notesFile string
	var dryRun, skipStore bool
	cmd := &cobra.Command{
		Use:   "publish",
		Short: "Publish a new version: store page, images, trailer and your web build",
		Long: `Publish checks everything first, then uploads the store page (text, banner, screenshots, trailer) and
your web export as a new version. Each version can be published once; bump "version" in vapour.toml
(or pass --version) for the next release. Release notes come from --notes, --notes-file or store.notes.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			out := cmd.OutOrStdout()
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

			var bundle *storepage.Bundle
			if !skipStore {
				if bundle, err = storepage.Load(dir, m); err != nil {
					return err
				}
			}
			releaseNotes, err := resolveNotes(notes, notesFile, bundle)
			if err != nil {
				return err
			}

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
			fmt.Fprintf(out, "Packed %s (%.1f MiB) as version %s\n", m.Build.Dir, float64(st.Size())/(1<<20), m.Project.Version)
			printBundle(out, bundle)
			if dryRun {
				fmt.Fprintln(out, "Dry run: nothing uploaded.")
				return nil
			}

			c, err := client(true)
			if err != nil {
				return err
			}
			if err := ensureProject(cmd.Context(), c, m, dir); err != nil {
				return err
			}
			// A version can be published once: check the existing releases before
			// uploading anything, so a repeat fails fast and tells the developer
			// to change the config.
			if err := checkVersionFree(cmd.Context(), c, m.Project.ID, m.Project.Version); err != nil {
				return err
			}
			// Store page first: if its media is rejected we stop before a version is used up.
			if bundle != nil {
				if err := pushStore(cmd.Context(), out, c, m, bundle); err != nil {
					return err
				}
			}

			f, err := os.Open(zipPath)
			if err != nil {
				return err
			}
			defer f.Close()
			fmt.Fprintf(out, "Uploading version %s to %s ...\n", m.Project.Version, c.Base)
			res, err := c.Publish(cmd.Context(), m.Project.ID, m.Project.Version, releaseNotes, api.File{Name: "build.zip", Body: f})
			if err != nil {
				var ae *api.Error
				if errors.As(err, &ae) && ae.Code == "version_exists" {
					return fmt.Errorf("version %s is already published. Bump \"version\" in vapour.toml (or pass --version) and run publish again", m.Project.Version)
				}
				return err
			}
			fmt.Fprintf(out, "Published %s v%s\n%s\n", res.Game.Title, res.Game.Version, res.URL)
			return nil
		},
	}
	cmd.Flags().StringVar(&dir, "dir", ".", "game folder containing vapour.toml")
	cmd.Flags().StringVar(&version, "version", "", "override project.version for this publish")
	cmd.Flags().StringVar(&notes, "notes", "", "release notes (Markdown) for this version")
	cmd.Flags().StringVar(&notesFile, "notes-file", "", "read the release notes (Markdown) from a file")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "check and zip everything, but do not upload")
	cmd.Flags().BoolVar(&skipStore, "skip-store", false, "publish the build only; leave the store page as it is")
	return cmd
}

// checkVersionFree rejects a version that is already published before anything is uploaded.
func checkVersionFree(ctx context.Context, c *api.Client, projectID, version string) error {
	g, err := c.GetProject(ctx, projectID)
	if err != nil {
		return err
	}
	for _, r := range g.Releases {
		if r.Version == version {
			return fmt.Errorf("version %s is already published. Bump \"version\" in vapour.toml (or pass --version) and run publish again", version)
		}
	}
	return nil
}

// resolveNotes picks the release notes: --notes-file, then --notes, then store.notes from vapour.toml.
func resolveNotes(inline, file string, b *storepage.Bundle) (string, error) {
	s := inline
	if file != "" {
		raw, err := os.ReadFile(file)
		if err != nil {
			return "", fmt.Errorf("release notes: %w", err)
		}
		s = string(raw)
	}
	if s == "" && b != nil {
		s = b.Notes
	}
	if utf8.RuneCountInString(s) > storepage.MaxNotes {
		return "", fmt.Errorf("release notes have %d characters; the limit is %d", utf8.RuneCountInString(s), storepage.MaxNotes)
	}
	return s, nil
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
