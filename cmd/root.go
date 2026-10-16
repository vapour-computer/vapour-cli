// Package cmd defines the vapour command-line interface.
package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"github.com/vapour-computer/vapour-cli/internal/api"
	"github.com/vapour-computer/vapour-cli/internal/config"
)

// Version is set at release time via -ldflags.
var Version = "dev"

// DocsURL is the developer documentation site. It is printed with every error.
const DocsURL = "https://vapour-dev-docs.pages.dev/"

// ErrorMessage formats an error for the terminal and points to the docs.
func ErrorMessage(err error) string {
	return "error: " + err.Error() + "\nNeed help? " + DocsURL
}

var apiFlag string
var allowCustomAPI bool
var allowInsecureHTTP bool

func newRoot() *cobra.Command {
	root := &cobra.Command{
		Use:   "vapour",
		Short: "Create and publish web games on Vapour",
		Long:  "Create and publish web games on Vapour.\n\nDocs: " + DocsURL,
		PersistentPreRun: func(cmd *cobra.Command, args []string) {
			maybeCheckForUpdate(cmd)
		},
		SilenceUsage:  true,
		SilenceErrors: true,
		Version:       Version,
	}
	defaultHelp := root.HelpFunc()
	root.SetHelpFunc(func(cmd *cobra.Command, args []string) {
		if cmd == root {
			out := cmd.OutOrStdout()
			blue := ""
			reset := ""
			if file, ok := out.(*os.File); ok {
				if info, err := file.Stat(); err == nil && info.Mode()&os.ModeCharDevice != 0 {
					blue, reset = "\x1b[34m", "\x1b[0m"
				}
			}
			fmt.Fprintf(out, "       %s.  : .\n        ' .%s\n  vapo%sur%s\n\n", blue, reset, blue, reset)
		}
		defaultHelp(cmd, args)
	})
	root.PersistentFlags().StringVar(&apiFlag, "api", "", "API base URL (default: saved login, or "+config.DefaultAPI+")")
	root.PersistentFlags().BoolVar(&allowCustomAPI, "allow-custom-api", false, "allow non-default API hosts (unsafe: tokens are sent to that host)")
	root.PersistentFlags().BoolVar(&allowInsecureHTTP, "allow-insecure-http", false, "allow plain HTTP only for localhost")
	root.AddCommand(newLogin(), newLogout(), newWhoami(), newCreate(), newTemplate(), newPublish(), newStore(), newBuild(), newTest(), newPerf(), newLLM(), newController(), newUpdate(), newScan(), newVerify())
	return root
}

func Execute() error { return newRoot().Execute() }

// client builds an API client from flags, env and saved credentials.
func client(requireToken bool) (*api.Client, error) {
	c, err := config.Load()
	if err != nil {
		return nil, err
	}
	tok := config.ResolveToken(c)
	if requireToken && tok == "" {
		return nil, fmt.Errorf("not logged in: run `vapour login --token <token>` (create a token in your dashboard) or set VAPOUR_TOKEN")
	}
	base, err := config.ValidateAPI(config.ResolveAPI(apiFlag, c), allowCustomAPI || os.Getenv("VAPOUR_ALLOW_CUSTOM_API") == "1", allowInsecureHTTP || os.Getenv("VAPOUR_ALLOW_INSECURE_HTTP") == "1")
	if err != nil {
		return nil, err
	}
	return api.New(base, tok, "vapour-cli/"+Version), nil
}
