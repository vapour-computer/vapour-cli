# `vapour skill install|update` (`cmd/skill.go`)

Installs the Vapour skill for Claude Code from `https://github.com/vapour-computer/vapour-skill.git`. It needs `git`; it does not call the Vapour API and needs no login.

- `install`: shallow-clones (`--depth 1`) into the target and **keeps `.git`** so it can be updated. Fails if the target exists unless `--force` (which removes it first).
- `update`: `git -C <target> pull --ff-only`. Errors if the target has no `.git` (run `install` first).

Target resolution (both commands): `--dest DIR` > `--project` (`./.claude/skills/vapour`) > `$CLAUDE_CONFIG_DIR/skills/vapour` > `~/.claude/skills/vapour`.

Flags: `--project`, `--dest`, and for `install` also `--repo` (default above), `--force`, `--allow-untrusted-template`.

Source safety reuses `trustedTemplateURL` (same rule as `create --from`): only `vapour-computer/*` GitHub repos (or local paths) unless `--allow-untrusted-template`. URLs starting with `-` are rejected.

Tests: `cmd/skill_test.go` (clone keeps `.git`, refuses overwrite, `--force`, untrusted source).
