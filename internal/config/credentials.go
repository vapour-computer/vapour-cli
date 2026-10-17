// Package config stores the CLI's login credentials in ~/.config/vapour/credentials.
package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/pelletier/go-toml/v2"
)

// DefaultAPI is used when neither a flag, VAPOUR_API nor saved credentials set one.
const DefaultAPI = "https://vapour-api-637456171586.asia-south1.run.app"

type Credentials struct {
	API   string `toml:"api,omitempty"`
	Token string `toml:"token"`
}

// Dir returns the config directory, honouring XDG_CONFIG_HOME.
func Dir() (string, error) {
	if x := os.Getenv("XDG_CONFIG_HOME"); x != "" {
		return filepath.Join(x, "vapour"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".config", "vapour"), nil
}

func Path() (string, error) {
	d, err := Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(d, "credentials"), nil
}

// Load reads saved credentials. A missing file returns an empty value and no error.
func Load() (Credentials, error) {
	var c Credentials
	p, err := Path()
	if err != nil {
		return c, err
	}
	b, err := os.ReadFile(p)
	if errors.Is(err, os.ErrNotExist) {
		return c, nil
	}
	if err != nil {
		return c, err
	}
	if err := toml.Unmarshal(b, &c); err != nil {
		return c, fmt.Errorf("%s: %w", p, err)
	}
	return c, nil
}

// Save writes credentials atomically with 0600 permissions.
func Save(c Credentials) error {
	p, err := Path()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		return err
	}
	b, err := toml.Marshal(c)
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(p), ".credentials-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(b); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), p)
}

// Remove deletes saved credentials. A missing file is not an error.
func Remove() error {
	p, err := Path()
	if err != nil {
		return err
	}
	if err := os.Remove(p); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

// ResolveAPI picks the API base URL: flag, then VAPOUR_API, then saved credentials, then the default.
func ResolveAPI(flag string, c Credentials) string {
	for _, v := range []string{flag, os.Getenv("VAPOUR_API"), c.API, DefaultAPI} {
		if v != "" {
			return v
		}
	}
	return DefaultAPI
}

// ResolveToken picks the token: VAPOUR_TOKEN (for CI), then saved credentials.
func ResolveToken(c Credentials) string {
	if t := os.Getenv("VAPOUR_TOKEN"); t != "" {
		return t
	}
	return c.Token
}
