package cmd

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"github.com/vapour-computer/vapour-cli/internal/project"
	"github.com/vapour-computer/vapour-cli/internal/zipsign"
)

type verifyResult struct {
	OK          bool   `json:"ok"`
	SHA256      string `json:"sha256"`
	KeyID       string `json:"key_id,omitempty"`
	Local       string `json:"local"`            // "verified" or the failure code
	Remote      string `json:"remote,omitempty"` // server status when --remote is used
	RemoteMatch *bool  `json:"remote_hash_matches,omitempty"`
	Message     string `json:"message,omitempty"`
}

func newVerify() *cobra.Command {
	var dir, attPath, version string
	var remote, asJSON bool
	cmd := &cobra.Command{
		Use:   "verify [zip]",
		Short: "Check a build zip against its signed attestation (and the server's record)",
		Long: `Verify recomputes the zip's SHA-256, checks the attestation's signature and confirms the zip still matches it.
The attestation defaults to <zip>.attestation.json, then to .vapour/build.zip.attestation.json in --dir.
With --remote the zip is optional: without it, verify checks the attestation's signature and the server's record only.

With --remote it also asks the server for the record of one published version (needs login) and checks that the
server saw the same SHA-256 and marked the version verified. Exit status is 1 on any mismatch.`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			out := cmd.OutOrStdout()
			zipPath := ""
			if len(args) == 1 {
				zipPath = args[0]
			} else if !remote {
				return errors.New("pass the zip to check, or use --remote to check the server's record")
			}
			if attPath == "" {
				attPath = attestationPath(dir)
				if zipPath != "" {
					if p := zipPath + zipsign.FileName; fileExists(p) {
						attPath = p
					}
				}
			}
			raw, err := os.ReadFile(attPath)
			if err != nil {
				return fmt.Errorf("cannot read the attestation %q: %w. Pass --attestation <file>", attPath, err)
			}
			att, err := zipsign.Parse(raw)
			if err != nil {
				return err
			}
			res := verifyResult{SHA256: att.SHA256, KeyID: att.KeyID, Local: "verified"}
			var failure error
			verify := func() error { return zipsign.VerifySignature(att) }
			if zipPath != "" {
				verify = func() error { return zipsign.Verify(zipPath, att) }
			}
			if err := verify(); err != nil {
				var ze *zipsign.Error
				if !errors.As(err, &ze) {
					return err
				}
				res.Local, res.Message, failure = ze.Code, ze.Message, err
			}
			if remote && failure == nil {
				m, err := project.Load(dir)
				if err != nil {
					return err
				}
				if m.Project.ID == "" {
					return errors.New("this project has no id yet: publish it once first")
				}
				v := version
				if v == "" {
					v = m.Project.Version
				}
				if v == "" {
					return errors.New("--version is required with --remote")
				}
				c, err := client(true)
				if err != nil {
					return err
				}
				rec, err := c.VersionVerification(cmd.Context(), m.Project.ID, v)
				if err != nil {
					return err
				}
				match := strings.EqualFold(rec.SHA256, att.SHA256)
				res.Remote, res.RemoteMatch = rec.Status, &match
				switch {
				case rec.Status != "verified":
					failure = fmt.Errorf("the server marked version %s as %q", v, rec.Status)
				case !match:
					failure = fmt.Errorf("the server recorded SHA-256 %s for version %s, but your zip is %s", rec.SHA256, v, att.SHA256)
				}
				if failure != nil {
					res.Message = failure.Error()
				}
			}
			res.OK = failure == nil
			if asJSON {
				b, _ := json.MarshalIndent(res, "", "  ")
				fmt.Fprintln(out, string(b))
			} else if res.OK {
				if zipPath == "" {
					fmt.Fprintf(out, "OK: signature valid (SHA-256 %s, key %s, scan %s)\n", att.SHA256, att.KeyID, att.Scan.Result)
				} else {
					fmt.Fprintf(out, "OK: signature valid, SHA-256 %s matches (key %s, scan %s)\n", att.SHA256, att.KeyID, att.Scan.Result)
				}
				if remote {
					fmt.Fprintf(out, "OK: the server recorded the same hash and marked the version %s\n", res.Remote)
				}
			}
			if failure != nil {
				return fmt.Errorf("verification failed: %w", failure)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&dir, "dir", ".", "game folder containing vapour.toml")
	cmd.Flags().StringVar(&attPath, "attestation", "", "attestation file (default: <zip>.attestation.json)")
	cmd.Flags().BoolVar(&remote, "remote", false, "also compare with the server's record (needs login)")
	cmd.Flags().StringVar(&version, "version", "", "published version to compare with (default: version in vapour.toml)")
	cmd.Flags().BoolVar(&asJSON, "json", false, "print the result as JSON")
	return cmd
}
