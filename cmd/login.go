package cmd

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"github.com/vapour-computer/vapour-cli/internal/api"
	"github.com/vapour-computer/vapour-cli/internal/config"
)

func newLogin() *cobra.Command {
	var token string
	var noVerify bool
	cmd := &cobra.Command{
		Use:   "login --token <token>",
		Short: "Save a personal access token (create one in your Vapour dashboard)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if token == "-" { // read from stdin so the token stays out of shell history
				s, err := bufio.NewReader(os.Stdin).ReadString('\n')
				if err != nil && s == "" {
					return fmt.Errorf("no token on stdin")
				}
				token = s
			}
			token = strings.TrimSpace(token)
			if token == "" {
				return fmt.Errorf("--token is required (use --token - to read it from stdin)")
			}
			if !strings.HasPrefix(token, "vpr_") {
				return fmt.Errorf("that does not look like a Vapour token (they start with vpr_)")
			}
			saved, err := config.Load()
			if err != nil {
				return err
			}
			base := config.ResolveAPI(apiFlag, saved)
			validated, err := config.ValidateAPI(base, allowCustomAPI || os.Getenv("VAPOUR_ALLOW_CUSTOM_API") == "1", allowInsecureHTTP || os.Getenv("VAPOUR_ALLOW_INSECURE_HTTP") == "1")
			if err != nil {
				return err
			}
			who := ""
			if !noVerify {
				me, err := api.New(validated, token, "vapour-cli/"+Version).Me(cmd.Context())
				if err != nil {
					return fmt.Errorf("token check failed: %w", err)
				}
				who = fmt.Sprintf(" as %s (%s)", me.User.Name, me.User.Role)
			}
			creds := config.Credentials{Token: token}
			if apiFlag != "" || os.Getenv("VAPOUR_API") != "" || saved.API != "" {
				creds.API = validated
			}
			if err := config.Save(creds); err != nil {
				return err
			}
			p, _ := config.Path()
			fmt.Fprintf(cmd.OutOrStdout(), "Logged in%s. Saved to %s\n", who, p)
			return nil
		},
	}
	cmd.Flags().StringVar(&token, "token", "", "personal access token (vpr_...), or - to read stdin")
	cmd.Flags().BoolVar(&noVerify, "no-verify", false, "save the token without checking it with the server")
	return cmd
}

func newLogout() *cobra.Command {
	return &cobra.Command{
		Use:   "logout",
		Short: "Remove the saved token",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := config.Remove(); err != nil {
				return err
			}
			fmt.Fprintln(cmd.OutOrStdout(), "Logged out.")
			return nil
		},
	}
}

func newWhoami() *cobra.Command {
	return &cobra.Command{
		Use:   "whoami",
		Short: "Show the account the saved token belongs to",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := client(true)
			if err != nil {
				return err
			}
			me, err := c.Me(cmd.Context())
			if err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "%s <%s> (%s) on %s\n", me.User.Name, me.User.Email, me.User.Role, c.Base)
			return nil
		},
	}
}
