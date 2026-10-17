package cmd

import (
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"
	"github.com/vapour-computer/vapour-cli/internal/guardrails"
	"github.com/vapour-computer/vapour-cli/internal/perfcheck"
	"github.com/vapour-computer/vapour-cli/internal/project"
)

func newPerf() *cobra.Command {
	var dir, engine string
	cmd := &cobra.Command{
		Use:   "perf",
		Short: "Show web performance guidance and project hints",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			m, err := project.Load(dir)
			if err != nil {
				return err
			}
			if err := m.Validate(); err != nil {
				return err
			}
			eng := strings.ToLower(strings.TrimSpace(engine))
			if eng == "" {
				eng = strings.ToLower(strings.TrimSpace(m.Project.Engine))
			}
			if !isPerfEngine(eng) {
				return fmt.Errorf("--engine must be one of godot, unity, bevy")
			}
			out := cmd.OutOrStdout()
			fmt.Fprintf(out, "Performance guidance for %s\n", eng)
			for _, line := range perfGuidance(eng) {
				fmt.Fprintf(out, "- %s\n", line)
			}
			findings := perfcheck.Check(dir, m, eng)
			if len(findings) == 0 {
				fmt.Fprintln(out, "No perf hints found in current files.")
				return nil
			}
			fmt.Fprintln(out, "Findings:")
			for _, line := range perfcheck.FormatWarnings(findings) {
				fmt.Fprintf(out, "- %s\n", line)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&dir, "dir", ".", "game folder containing vapour.toml")
	cmd.Flags().StringVar(&engine, "engine", "", "engine override: godot | unity | bevy (default: vapour.toml)")
	return cmd
}

func emitPerfHints(w io.Writer, dir string, m *project.Manifest, noPerfHints bool) {
	if noPerfHints {
		return
	}
	for _, line := range perfcheck.FormatWarnings(perfcheck.Check(dir, m, "")) {
		fmt.Fprintf(w, "Warning: perf %s\n", line)
	}
}

// emitGuardrails prints guardrail findings and fails when a blocking one is not allowed in vapour.toml.
func emitGuardrails(w io.Writer, dir string, m *project.Manifest, chunked bool) error {
	findings := guardrails.Check(dir, m, guardrails.Options{Chunked: chunked})
	for _, line := range guardrails.Format(findings) {
		fmt.Fprintf(w, "Guardrail: %s\n", line)
	}
	if blocked := guardrails.Blocking(findings, m.Guardrails.Allow); len(blocked) > 0 {
		codes := make([]string, len(blocked))
		for i, f := range blocked {
			codes[i] = f.Code
		}
		return fmt.Errorf("blocked by guardrails: %s (fix them, or acknowledge with [guardrails] allow in %s)", strings.Join(codes, ", "), project.File)
	}
	return nil
}

func isPerfEngine(engine string) bool {
	switch engine {
	case "godot", "unity", "bevy":
		return true
	default:
		return false
	}
}

func perfGuidance(engine string) []string {
	common := []string{
		"Check chrome://gpu and confirm hardware acceleration.",
		"On dual-GPU laptops, set the browser to High performance.",
	}
	switch engine {
	case "godot":
		return append([]string{
			"Enable web thread support (`variant/thread_support=true`).",
			"Use `.web` overrides for quality (including bilinear scaling and 0.75 render scale).",
			"Use Jolt Physics for 3D projects.",
			"For custom templates: `scons platform=web target=template_release production=yes optimize=speed lto=full threads=yes`.",
			"Use VRAM texture compression and keep WebGL2 as the target (Godot web does not support WebGPU).",
		}, common...)
	case "unity":
		return append([]string{
			"Use a non-development build.",
			"Set Code Optimization to Runtime Speed.",
			"Enable Brotli/Gzip compression for web build files.",
			"On Unity 6, put WebGPU above WebGL2 in Graphics APIs.",
		}, common...)
	case "bevy":
		return append([]string{
			"Use a speed profile for web (`opt-level = 3`, `lto`, `codegen-units = 1`).",
			"Use Trunk rust link with `data-wasm-opt`.",
			"Use optional `webgpu` feature only with a planned WebGL2 fallback build.",
		}, common...)
	default:
		return common
	}
}
