package zipsign

import (
	"crypto/ed25519"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/vapour-computer/vapour-cli/internal/zipscan"
)

func writeZip(t *testing.T, content string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "build.zip")
	if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func newKey(t *testing.T) ed25519.PrivateKey {
	t.Helper()
	_, k, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	return k
}

func TestSignVerifyRoundTrip(t *testing.T) {
	p := writeZip(t, "zip bytes")
	a, err := Sign(p, &zipscan.Report{Entries: 3}, newKey(t), time.Unix(0, 0))
	if err != nil {
		t.Fatal(err)
	}
	if a.Scan.Result != "pass" || a.Scan.Findings == nil {
		t.Fatalf("scan = %+v", a.Scan)
	}
	b, _ := a.JSON()
	parsed, err := Parse(b)
	if err != nil {
		t.Fatal(err)
	}
	if err := Verify(p, parsed); err != nil {
		t.Fatalf("verify: %v", err)
	}
}

func TestVerifyDetectsChanges(t *testing.T) {
	p := writeZip(t, "zip bytes")
	a, _ := Sign(p, &zipscan.Report{}, newKey(t), time.Now())

	if err := os.WriteFile(p, []byte("zip bytez"), 0o600); err != nil {
		t.Fatal(err)
	}
	var e *Error
	if err := Verify(p, a); !asErr(err, &e) || e.Code != "hash_mismatch" {
		t.Fatalf("changed zip: %v", err)
	}
	if err := os.WriteFile(p, []byte("zip bytes plus"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := Verify(p, a); !asErr(err, &e) || e.Code != "size_mismatch" {
		t.Fatalf("longer zip: %v", err)
	}

	p = writeZip(t, "zip bytes")
	tampered := *a
	tampered.Scan.Result = "pass"
	tampered.Entries = 99
	if err := Verify(p, &tampered); !asErr(err, &e) || e.Code != "bad_signature" {
		t.Fatalf("edited attestation: %v", err)
	}
	other := *a
	other.KeyID = "0000000000000000"
	if err := Verify(p, &other); !asErr(err, &e) || e.Code != "bad_key_id" {
		t.Fatalf("wrong key id: %v", err)
	}
}

func TestSignRefusesFailedScan(t *testing.T) {
	p := writeZip(t, "x")
	rep := &zipscan.Report{Findings: []zipscan.Finding{{Severity: zipscan.SeverityError, Code: "x"}}}
	if _, err := Sign(p, rep, newKey(t), time.Now()); err == nil {
		t.Fatal("signed a failed build")
	}
}

func TestWarnResult(t *testing.T) {
	p := writeZip(t, "x")
	rep := &zipscan.Report{Findings: []zipscan.Finding{{Severity: zipscan.SeverityWarn, Code: "w"}}}
	a, err := Sign(p, rep, newKey(t), time.Now())
	if err != nil || a.Scan.Result != "warn" {
		t.Fatalf("%v %+v", err, a)
	}
}

func TestKeyCreatedOnceWithPrivateMode(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	k1, created, err := LoadOrCreateKey()
	if err != nil || !created {
		t.Fatalf("first: %v %v", created, err)
	}
	k2, created, err := LoadOrCreateKey()
	if err != nil || created || !k1.Equal(k2) {
		t.Fatalf("second: %v %v", created, err)
	}
	path, _ := KeyPath()
	st, _ := os.Stat(path)
	if st.Mode().Perm() != 0o600 {
		t.Fatalf("mode = %o", st.Mode().Perm())
	}
	if err := os.Chmod(path, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, _, err := LoadOrCreateKey(); err == nil || !strings.Contains(err.Error(), "chmod 600") {
		t.Fatalf("loose mode accepted: %v", err)
	}
}

func TestCanonicalFormIsStable(t *testing.T) {
	p := writeZip(t, "x")
	a, _ := Sign(p, &zipscan.Report{}, newKey(t), time.Unix(0, 0))
	b1, _ := a.signedBytes()
	a.Signature = "ignored"
	b2, _ := a.signedBytes()
	if string(b1) != string(b2) || !strings.HasPrefix(string(b1), domain+`{"entries":0,"key_id":`) {
		t.Fatalf("canonical form: %s", b1)
	}
}

func asErr(err error, target **Error) bool {
	e, ok := err.(*Error)
	if ok {
		*target = e
	}
	return ok
}
