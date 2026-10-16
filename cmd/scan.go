package cmd

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
	"github.com/vapour-computer/vapour-cli/internal/project"
	"github.com/vapour-computer/vapour-cli/internal/zipdir"
	"github.com/vapour-computer/vapour-cli/internal/zipscan"
)

func newScan() *cobra.Command {
	var dir string
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "scan [zip-or-folder]",
		Short: "Scan a build zip for zip bombs, unsafe paths and native or suspicious files",
		Long: `Scan statically analyzes a build without extracting or running anything. It checks for zip bombs, unsafe paths,
symlinks, native executables (even when renamed), known in-browser miners and the EICAR test string.

It is a heuristic check, not an antivirus. "vapour publish" runs the same scan automatically and cancels the upload
if it finds an error. With no argument, scan uses build.dir from vapour.toml. Exit status is 1 if any error is found.`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			out := cmd.OutOrStdout()
			target := ""
			if len(args) == 1 {
				target = args[0]
			} else {
				m, err := project.Load(dir)
				if err != nil {
					return err
				}
				target = filepath.Join(dir, m.Build.Dir)
			}
			st, err := os.Stat(target)
			if err != nil {
				return fmt.Errorf("%q not found", target)
			}
			zipPath := target
			if st.IsDir() {
				tmp, err := os.CreateTemp("", "vapour-scan-*.zip")
				if err != nil {
					return err
				}
				defer os.Remove(tmp.Name())
				if _, err := zipdir.Zip(target, io.Writer(tmp)); err != nil {
					tmp.Close()
					return err
				}
				if err := tmp.Close(); err != nil {
					return err
				}
				zipPath = tmp.Name()
			}
			if asJSON {
				rep, err := zipscan.ScanFile(zipPath, zipscan.DefaultLimits())
				if err != nil {
					return err
				}
				if rep.Findings == nil {
					rep.Findings = []zipscan.Finding{}
				}
				b, _ := json.MarshalIndent(rep, "", "  ")
				fmt.Fprintln(out, string(b))
				if rep.Failed() {
					return fmt.Errorf("%d problem(s) found", countErrors(rep))
				}
				return nil
			}
			rep, err := scanZip(out, zipPath)
			if err != nil {
				return err
			}
			if len(rep.Findings) == 0 {
				fmt.Fprintln(out, "No problems found.")
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&dir, "dir", ".", "game folder containing vapour.toml")
	cmd.Flags().BoolVar(&asJSON, "json", false, "print the report as JSON")
	return cmd
}
