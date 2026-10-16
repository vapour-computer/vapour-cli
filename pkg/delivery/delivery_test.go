package delivery

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func TestSplitAndDelta(t *testing.T) {
	data := bytes.Repeat([]byte("repeatable game asset"), 1<<20)
	var rebuilt []byte
	max := 0
	if e := Split(bytes.NewReader(data), func(b []byte) error {
		rebuilt = append(rebuilt, b...)
		if len(b) > max {
			max = len(b)
		}
		return nil
	}); e != nil {
		t.Fatal(e)
	}
	if !bytes.Equal(data, rebuilt) || max > MaxChunk {
		t.Fatal("split corrupt or unbounded")
	}
	root := t.TempDir()
	cache := t.TempDir()
	os.WriteFile(filepath.Join(root, "index.html"), []byte("hello"), 0600)
	os.WriteFile(filepath.Join(root, "asset.bin"), data, 0600)
	a, e := Pack(root, cache, "v1", []string{"*.html"})
	if e != nil {
		t.Fatal(e)
	}
	os.WriteFile(filepath.Join(root, "index.html"), []byte("changed"), 0600)
	b, e := Pack(root, cache, "v2", []string{"*.html"})
	if e != nil {
		t.Fatal(e)
	}
	if fmt.Sprint(a.Files[0].Chunks) != fmt.Sprint(b.Files[0].Chunks) {
		t.Fatal("unchanged asset changed")
	}
	for _, f := range b.Files {
		for _, c := range f.Chunks {
			raw, e := os.ReadFile(filepath.Join(cache, c.Hash))
			if e != nil {
				t.Fatal(e)
			}
			if fmt.Sprintf("%x", sha256.Sum256(raw)) != c.Hash {
				t.Fatal("hash mismatch")
			}
		}
	}
	if !b.Files[1].Boot {
		t.Fatal("boot glob")
	}
}
func TestRejectSymlink(t *testing.T) {
	root := t.TempDir()
	os.Symlink("/etc/passwd", filepath.Join(root, "escape"))
	if _, e := Pack(root, t.TempDir(), "v1", nil); e == nil {
		t.Fatal("accepted symlink")
	}
}

func TestRepairCorruptSpool(t *testing.T) {
	root, cache := t.TempDir(), t.TempDir()
	os.WriteFile(filepath.Join(root, "index.html"), []byte("original"), 0600)
	m, e := Pack(root, cache, "v1", nil)
	if e != nil {
		t.Fatal(e)
	}
	p := filepath.Join(cache, m.Files[0].Chunks[0].Hash)
	os.WriteFile(p, []byte("truncated"), 0600)
	if _, e := Pack(root, cache, "v2", nil); e != nil {
		t.Fatal(e)
	}
	b, e := os.ReadFile(p)
	if e != nil || string(b) != "original" {
		t.Fatal("corrupt cached chunk reused", e)
	}
}
