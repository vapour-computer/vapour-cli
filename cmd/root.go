// Package cmd defines the vapour command-line interface.
package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
	"github.com/vapour-computer/vapour-cli/internal/api"
	"github.com/vapour-computer/vapour-cli/internal/config"
)

// Version is set at release time via -ldflags.
var Version = "dev"

var apiFlag string

func newRoot() *cobra.Command {
	root := &cobra.Command{
		Use:           "vapour",
		Short:         "Create and publish web games on Vapour",
		SilenceUsage:  true,
		SilenceErrors: true,
		Version:       Version,
	}
	root.PersistentFlags().StringVar(&apiFlag, "api", "", "API base URL (default: saved login, or "+config.DefaultAPI+")")
	root.AddCommand(newLogin(), newLogout(), newWhoami(), newCreate(), newPublish())
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
	return api.New(config.ResolveAPI(apiFlag, c), tok, "vapour-cli/"+Version), nil
}
