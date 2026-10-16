package cmd

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"github.com/spf13/cobra"
	"github.com/vapour-computer/vapour-cli/internal/api"
	"github.com/vapour-computer/vapour-cli/internal/project"
	"github.com/vapour-computer/vapour-cli/internal/storepage"
	"github.com/vapour-computer/vapour-cli/internal/zipdir"
	"github.com/vapour-computer/vapour-cli/pkg/delivery"
)

const maxBuild = 512 << 20 // matches the API limit

func newPublish() *cobra.Command {
	var dir, version, notes, notesFile string
	var dryRun, skipStore, noPerfHints bool
	var chunked bool
	cmd := &cobra.Command{
		Use:   "publish",
		Short: "Publish a new version: store page, images, trailer and your web build",
		Long: `Publish checks everything first, then uploads the store page (text, banner, screenshots, trailer) and
your web export as a new version. Each version can be published once, so pass a new --version each release.
Release notes are required with --notes or --notes-file.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			out := cmd.OutOrStdout()
			m, err := project.Load(dir)
			if err != nil {
				return err
			}
			if version == "" {
				return fmt.Errorf("--version is required (e.g. --version 0.3.1)")
			}
			m.Project.Version = version
			if err := m.Validate(); err != nil {
				return err
			}
			if strings.TrimSpace(m.Store.Notes) != "" {
				fmt.Fprintln(out, "Warning: store.notes in vapour.toml is ignored by publish; use --notes or --notes-file.")
			}
			emitPerfHints(out, dir, m, noPerfHints)

			var bundle *storepage.Bundle
			if !skipStore {
				if bundle, err = storepage.Load(dir, m); err != nil {
					return err
				}
			}
			releaseNotes, err := resolveNotes(notes, notesFile)
			if err != nil {
				return err
			}

			// Resolve the build: a folder is checked and zipped; a .zip is uploaded as is.
			buildPath, err := project.ResolvePathInside(dir, m.Build.Dir, "build.dir")
			if err != nil {
				return err
			}
			if chunked {
				if err := scanFolder(out, buildPath); err != nil {
					return err
				}
				fmt.Fprintln(out, "Note: --experimental-chunks sends no signed attestation; the server checks the chunks itself.")
				cache, err := os.MkdirTemp("", "vapour-chunks-*")
				if err != nil {
					return err
				}
				defer os.RemoveAll(cache)
				manifest, err := delivery.Pack(buildPath, cache, m.Project.Version, []string{"*.html", "*.js", "*.wasm", "*.pck", "Build/*.js", "Build/*.wasm", "Build/*.data", "Build/*.unityweb", "Build/*.gz", "Build/*.br"})
				if err != nil {
					return err
				}
				if raw, e := os.ReadFile(filepath.Join(dir, ".vapour", "build.json")); e == nil {
					var meta struct {
						Schema int             `json:"schema"`
						Files  []delivery.File `json:"files"`
					}
					if e := json.Unmarshal(raw, &meta); e != nil {
						return fmt.Errorf("build metadata: %w", e)
					}
					if meta.Schema != 1 {
						return fmt.Errorf("unsupported build metadata schema")
					}
					flags := map[string]delivery.File{}
					for _, f := range meta.Files {
						flags[f.Path] = f
					}
					for i, f := range manifest.Files {
						if entry, ok := flags[f.Path]; ok {
							if entry.Size != f.Size {
								return fmt.Errorf("stale build metadata for %s; rerun vapour build", f.Path)
							}
							manifest.Files[i].Boot = entry.Boot
						}
					}
				} else if !os.IsNotExist(e) {
					return e
				}
				for _, f := range manifest.Files {
					if f.Size > 1<<30 {
						fmt.Fprintf(out, "Warning: %s exceeds 1 GiB; split engine packs before browser loading: %s\n", f.Path, DocsURL)
					}
				}
				if dryRun {
					fmt.Fprintf(out, "Chunked dry run: %d files, nothing uploaded.\n", len(manifest.Files))
					return nil
				}
				fmt.Fprintln(out, "Warning: release notes are not sent with --experimental-chunks")
				c, err := client(true)
				if err != nil {
					return err
				}
				if err := ensureProject(cmd.Context(), c, m, dir); err != nil {
					return err
				}
				if bundle != nil {
					if err := pushStore(cmd.Context(), out, c, m, bundle); err != nil {
						return err
					}
				}
				n, err := delivery.Upload(cmd.Context(), c.Base, m.Project.ID, c.Token, cache, manifest)
				if err != nil {
					return err
				}
				fmt.Fprintf(out, "Finalized chunk manifest %s; uploaded %d bytes\n", manifest.Version, n)
				return nil
			}
			zipPath, cleanup, err := prepareZip(buildPath, m.Build.PointerLockEnabled(), m.Build.PointerLockPause(), m.Runtime.Entry)
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
			// Static analysis runs before anything is sent. Any error finding stops here: no upload, no version used.
			report, err := scanZip(out, zipPath)
			if err != nil {
				return err
			}
			attestation, err := signZip(out, zipPath, dir, report)
			if err != nil {
				return err
			}
			printBundle(out, bundle)
			meta, err := publishMetadata(m, dir)
			if err != nil {
				return err
			}
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
			res, err := c.Publish(cmd.Context(), m.Project.ID, m.Project.Version, releaseNotes, meta, api.File{Name: "build.zip", Body: f}, attestation)
			if err != nil {
				var ae *api.Error
				if errors.As(err, &ae) && ae.Code == "version_exists" {
					return fmt.Errorf("version %s is already published. Pass a new --version and run publish again", m.Project.Version)
				}
				return err
			}
			fmt.Fprintf(out, "Uploaded %s v%s\n%s\n", res.Game.Title, m.Project.Version, res.URL)
			fmt.Fprintf(out, "The server now verifies the zip, then a Vapour admin reviews this version before it goes live.\nCheck progress: vapour verify --remote --version %s\n", m.Project.Version)
			return nil
		},
	}
	cmd.Flags().StringVar(&dir, "dir", ".", "game folder containing vapour.toml")
	cmd.Flags().StringVar(&version, "version", "", "version to publish (required; e.g. 0.3.1)")
	cmd.Flags().StringVar(&notes, "notes", "", "release notes (Markdown); required unless --notes-file is used")
	cmd.Flags().StringVar(&notesFile, "notes-file", "", "read release notes (Markdown) from a file; required unless --notes is used")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "check and zip everything, but do not upload")
	cmd.Flags().BoolVar(&skipStore, "skip-store", false, "publish the build only; leave the store page as it is")
	cmd.Flags().BoolVar(&noPerfHints, "no-perf-hints", false, "suppress performance hint warnings")
	cmd.Flags().BoolVar(&chunked, "experimental-chunks", false, "use experimental v1 chunk protocol (requires a compatible server)")
	return cmd
}

// resolveNotes validates and reads required release notes from exactly one source.
func resolveNotes(inline, file string) (string, error) {
	const required = `release notes are required: pass --notes "..." or --notes-file CHANGELOG.md`
	if inline != "" && file != "" {
		return "", fmt.Errorf("pass exactly one of --notes or --notes-file")
	}
	if inline == "" && file == "" {
		return "", fmt.Errorf(required)
	}
	s := inline
	if file != "" {
		raw, err := os.ReadFile(file)
		if err != nil {
			return "", fmt.Errorf("release notes file %q: %w", file, err)
		}
		s = string(raw)
	}
	if strings.TrimSpace(s) == "" {
		if file != "" {
			return "", fmt.Errorf("release notes file %q is empty", file)
		}
		return "", fmt.Errorf(required)
	}
	if utf8.RuneCountInString(s) > storepage.MaxNotes {
		return "", fmt.Errorf("release notes have %d characters; the limit is %d", utf8.RuneCountInString(s), storepage.MaxNotes)
	}
	return s, nil
}

func publishMetadata(m *project.Manifest, dir string) (map[string]any, error) {
	runtime := map[string]any{}
	if m.Runtime.Threads {
		runtime["threads"] = true
	}
	if m.Runtime.WebGPU {
		runtime["webgpu"] = true
	}
	if m.Runtime.COEP != "" {
		runtime["coep"] = m.Runtime.COEP
	}
	if m.Runtime.Entry != "" {
		runtime["entry"] = m.Runtime.Entry
	}
	if m.Runtime.OnQuit != "" {
		runtime["on_quit"] = m.Runtime.OnQuit
	}
	mobile := map[string]any{}
	if orientation := m.MobileOrientation(); orientation != "any" {
		mobile["orientation"] = orientation
	}
	if scale := m.MobileRenderScale(); scale != 1 {
		mobile["render_scale"] = scale
	}
	if m.Mobile.MaxDPR != nil {
		mobile["max_dpr"] = *m.Mobile.MaxDPR
	}
	rotate, err := m.RotateMessageHTML(dir)
	if err != nil {
		return nil, err
	}
	if rotate != "" {
		mobile["rotate_message"] = rotate
	}
	out := map[string]any{}
	if len(runtime) > 0 {
		out["runtime"] = runtime
	}
	if len(mobile) > 0 {
		out["mobile"] = mobile
	}
	if len(m.Controller.Controls) > 0 {
		out["controller"] = map[string]any{
			"mode":     m.Controller.CanonicalMode(),
			"controls": m.Controller.Controls,
		}
	}
	return out, nil
}

// prepareZip returns a path to a zip of buildPath and a cleanup function.
func prepareZip(buildPath string, pointerLock bool, pointerLockPause bool, entry string) (string, func(), error) {
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
	if _, err := zipdir.ZipWith(buildPath, io.Writer(tmp), zipdir.Options{PointerLock: pointerLock, PointerLockPause: pointerLockPause, PointerLockEntry: entry}); err != nil {
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
