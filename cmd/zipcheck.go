package cmd

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/vapour-computer/vapour-cli/internal/zipdir"
	"github.com/vapour-computer/vapour-cli/internal/zipscan"
	"github.com/vapour-computer/vapour-cli/internal/zipsign"
)

// attestationName is where publish writes the signed attestation, inside the game folder.
const attestationName = "build.zip" + zipsign.FileName

func attestationPath(gameDir string) string {
	return filepath.Join(gameDir, ".vapour", attestationName)
}

func printFindings(out io.Writer, rep *zipscan.Report) {
	for _, f := range rep.Findings {
		where := ""
		if f.Path != "" {
			where = " [" + f.Path + "]"
		}
		fmt.Fprintf(out, "  %-5s %s%s: %s\n", f.Severity, f.Code, where, f.Message)
	}
}

func countErrors(rep *zipscan.Report) int {
	n := 0
	for _, f := range rep.Findings {
		if f.Severity == zipscan.SeverityError {
			n++
		}
	}
	return n
}

// scanZip scans zipPath and prints the result. A build with any error finding returns an error, so nothing is uploaded.
func scanZip(out io.Writer, zipPath string) (*zipscan.Report, error) {
	rep, err := zipscan.ScanFile(zipPath, zipscan.DefaultLimits())
	if err != nil {
		return nil, err
	}
	fmt.Fprintf(out, "Scanned %d files (%.1f MiB packed, %.1f MiB unpacked)\n", rep.Entries, float64(rep.CompressedBytes)/(1<<20), float64(rep.UncompressedBytes)/(1<<20))
	printFindings(out, rep)
	if rep.Failed() {
		return rep, fmt.Errorf("build rejected: %d problem(s) found in the zip. Nothing was uploaded and no version number was used. Fix them and publish again (see the docs: games/zip-verification)", countErrors(rep))
	}
	return rep, nil
}

// signZip signs the scanned zip and writes the attestation next to the build. It returns the attestation JSON to upload.
func signZip(out io.Writer, zipPath, gameDir string, rep *zipscan.Report) ([]byte, error) {
	key, created, err := zipsign.LoadOrCreateKey()
	if err != nil {
		return nil, err
	}
	if created {
		p, _ := zipsign.KeyPath()
		fmt.Fprintf(out, "Created your signing key at %s. Keep it private; do not commit or share it.\n", p)
	}
	att, err := zipsign.Sign(zipPath, rep, key, time.Now())
	if err != nil {
		return nil, err
	}
	b, err := att.JSON()
	if err != nil {
		return nil, err
	}
	p := attestationPath(gameDir)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return nil, err
	}
	if err := os.WriteFile(p, append(b, '\n'), 0o644); err != nil {
		return nil, err
	}
	fmt.Fprintf(out, "Signed: SHA-256 %s (key %s)\nAttestation written to %s\n", att.SHA256, att.KeyID, p)
	return b, nil
}

// scanFolder scans a build folder (or a prebuilt zip) without uploading it, for paths that do not send a zip.
func scanFolder(out io.Writer, buildPath string) error {
	st, err := os.Stat(buildPath)
	if err != nil {
		return fmt.Errorf("build path %q not found: export your game for Web first", buildPath)
	}
	zipPath := buildPath
	if st.IsDir() {
		tmp, err := os.CreateTemp("", "vapour-scan-*.zip")
		if err != nil {
			return err
		}
		defer os.Remove(tmp.Name())
		if _, err := zipdir.Zip(buildPath, io.Writer(tmp)); err != nil {
			tmp.Close()
			return err
		}
		if err := tmp.Close(); err != nil {
			return err
		}
		zipPath = tmp.Name()
	}
	_, err = scanZip(out, zipPath)
	return err
}

func shortHash(s string) string {
	if len(s) > 12 {
		return s[:12]
	}
	return strings.TrimSpace(s)
}

func fileExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}
