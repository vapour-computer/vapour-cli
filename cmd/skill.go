package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
)

const (
	defaultSkillRepo = "https://github.com/vapour-computer/vapour-skill.git"
	skillDirName     = "vapour"
)

func newSkill() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "skill",
		Short: "Install the Vapour skill for Claude Code",
		Long:  "Install or update the Vapour skill, which teaches Claude Code how to build, test and publish games with this CLI.",
	}
	cmd.AddCommand(newSkillInstall(), newSkillUpdate())
	return cmd
}

func newSkillInstall() *cobra.Command {
	var project, force, allowUntrusted bool
	var dest, repo string
	cmd := &cobra.Command{
		Use:   "install",
		Short: "Clone the Vapour skill into your Claude skills folder",
		Long: "Clone " + defaultSkillRepo + " into ~/.claude/skills/vapour (or $CLAUDE_CONFIG_DIR/skills/vapour).\n" +
			"Use --project to install into ./.claude/skills/vapour for the current project only.\n" +
			"The clone keeps its .git folder so `vapour skill update` can pull new versions.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			target, err := skillTarget(dest, project)
			if err != nil {
				return err
			}
			if err := installSkill(repo, target, force, allowUntrusted); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Installed the Vapour skill at %s\nRestart Claude Code (or start a new session) to load it.\n", target)
			return nil
		},
	}
	cmd.Flags().BoolVar(&project, "project", false, "install into ./.claude/skills/vapour instead of your user folder")
	cmd.Flags().StringVar(&dest, "dest", "", "install into this exact folder")
	cmd.Flags().StringVar(&repo, "repo", defaultSkillRepo, "git URL of the skill repo")
	cmd.Flags().BoolVar(&force, "force", false, "replace an existing install")
	cmd.Flags().BoolVar(&allowUntrusted, "allow-untrusted-template", false, "allow cloning from outside the vapour-computer org")
	return cmd
}

func newSkillUpdate() *cobra.Command {
	var project bool
	var dest string
	cmd := &cobra.Command{
		Use:   "update",
		Short: "Pull the latest Vapour skill",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			target, err := skillTarget(dest, project)
			if err != nil {
				return err
			}
			if _, err := os.Stat(filepath.Join(target, ".git")); err != nil {
				return fmt.Errorf("no git-based skill install at %s: run `vapour skill install`", target)
			}
			if _, err := exec.LookPath("git"); err != nil {
				return fmt.Errorf("git is required to update the skill")
			}
			c := exec.Command("git", "-C", target, "pull", "--ff-only")
			c.Stdout = cmd.OutOrStdout()
			c.Stderr = os.Stderr
			if err := c.Run(); err != nil {
				return fmt.Errorf("git pull failed: %w", err)
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&project, "project", false, "update ./.claude/skills/vapour")
	cmd.Flags().StringVar(&dest, "dest", "", "update the skill in this exact folder")
	return cmd
}

// skillTarget resolves where the skill lives: --dest, then ./.claude (--project), then the user's Claude config dir.
func skillTarget(dest string, project bool) (string, error) {
	if dest != "" {
		return filepath.Abs(dest)
	}
	if project {
		wd, err := os.Getwd()
		if err != nil {
			return "", err
		}
		return filepath.Join(wd, ".claude", "skills", skillDirName), nil
	}
	base := os.Getenv("CLAUDE_CONFIG_DIR")
	if base == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		base = filepath.Join(home, ".claude")
	}
	return filepath.Join(base, "skills", skillDirName), nil
}

// installSkill shallow-clones repo into target and keeps .git so the install can be updated.
func installSkill(repo, target string, force, allowUntrusted bool) error {
	if strings.HasPrefix(repo, "-") {
		return fmt.Errorf("invalid git URL %q", repo)
	}
	if !allowUntrusted && !trustedTemplateURL(repo) {
		return fmt.Errorf("untrusted skill source %q (pass --allow-untrusted-template to override)", repo)
	}
	if _, err := exec.LookPath("git"); err != nil {
		return fmt.Errorf("git is required to install the skill")
	}
	if _, err := os.Lstat(target); err == nil {
		if !force {
			return fmt.Errorf("%s already exists: use `vapour skill update`, or pass --force to replace it", target)
		}
		if err := os.RemoveAll(target); err != nil {
			return err
		}
	}
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return err
	}
	c := exec.Command("git", "clone", "--depth", "1", "--", repo, target)
	c.Stderr = os.Stderr
	if err := c.Run(); err != nil {
		return fmt.Errorf("git clone failed: %w", err)
	}
	return nil
}
