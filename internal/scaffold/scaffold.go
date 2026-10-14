// Package scaffold writes a starter game folder from embedded templates.
package scaffold

import (
	"embed"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"text/template"
)

//go:embed all:templates
var templates embed.FS

type Data struct {
	Title  string
	Slug   string
	Engine string
}

// Write renders the template for engine into dir (which must be empty or not exist).
func Write(dir, engine string, d Data) error {
	root := "templates/" + engine
	if _, err := fs.Stat(templates, root); err != nil {
		root = "templates/other"
	}
	if entries, err := os.ReadDir(dir); err == nil && len(entries) > 0 {
		return fmt.Errorf("%s already exists and is not empty", dir)
	}
	return fs.WalkDir(templates, root, func(p string, e fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel := strings.TrimPrefix(strings.TrimPrefix(p, root), "/")
		if rel == "" {
			return os.MkdirAll(dir, 0o755)
		}
		dst := filepath.Join(dir, strings.TrimSuffix(rel, ".tmpl"))
		if e.IsDir() {
			return os.MkdirAll(dst, 0o755)
		}
		b, err := templates.ReadFile(p)
		if err != nil {
			return err
		}
		if strings.HasSuffix(rel, ".tmpl") {
			t, err := template.New(rel).Parse(string(b))
			if err != nil {
				return err
			}
			f, err := os.Create(dst)
			if err != nil {
				return err
			}
			defer f.Close()
			return t.Execute(f, d)
		}
		return os.WriteFile(dst, b, 0o644)
	})
}
