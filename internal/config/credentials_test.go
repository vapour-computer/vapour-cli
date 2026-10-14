package config

import (
	"os"
	"testing"
)

func TestSaveLoadPermsAndResolve(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("VAPOUR_API", "")
	t.Setenv("VAPOUR_TOKEN", "")
	if c, err := Load(); err != nil || c.Token != "" {
		t.Fatalf("empty load: %v %+v", err, c)
	}
	if err := Save(Credentials{API: "http://x", Token: "vpr_abc"}); err != nil {
		t.Fatal(err)
	}
	p, _ := Path()
	st, err := os.Stat(p)
	if err != nil || st.Mode().Perm() != 0o600 {
		t.Fatalf("perm: %v %v", err, st.Mode())
	}
	c, _ := Load()
	if c.Token != "vpr_abc" || c.API != "http://x" {
		t.Fatalf("load: %+v", c)
	}
	if ResolveAPI("flag", c) != "flag" || ResolveAPI("", c) != "http://x" || ResolveAPI("", Credentials{}) != DefaultAPI {
		t.Error("API precedence wrong")
	}
	t.Setenv("VAPOUR_TOKEN", "vpr_env")
	if ResolveToken(c) != "vpr_env" {
		t.Error("env token should win")
	}
	if err := Remove(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(p); !os.IsNotExist(err) {
		t.Error("not removed")
	}
}
