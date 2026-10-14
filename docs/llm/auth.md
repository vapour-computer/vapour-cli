# Auth: `login`, `logout`, `whoami` (`cmd/login.go`, `internal/config`)

- `vapour login --token vpr_...` saves a personal access token (created in the Vapour dashboard). Tokens must start
  with `vpr_`. `--token -` reads it from stdin (keeps it out of shell history). `--no-verify` skips the server check;
  otherwise `Me()` is called and "Logged in as NAME (ROLE)" is printed.
- The API URL is stored in the file only if `--api`, `VAPOUR_API` or a previously saved value set it.
- `vapour logout` deletes the credentials file.
- `vapour whoami` prints `name <email> (role) on <api>`.
- CI: set `VAPOUR_TOKEN` instead of logging in; it takes priority over the file.
