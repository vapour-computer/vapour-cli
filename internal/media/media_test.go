package media

import (
	"bytes"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writePNG(t *testing.T, dir, name string, w, h int) string {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	img.Set(0, 0, color.RGBA{255, 0, 0, 255})
	var b bytes.Buffer
	if err := png.Encode(&b, img); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, b.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestImageRules(t *testing.T) {
	d := t.TempDir()
	if _, err := CheckImage(writePNG(t, d, "banner.png", 1920, 620), Banner); err != nil {
		t.Fatalf("good banner rejected: %v", err)
	}
	if _, err := CheckImage(writePNG(t, d, "small.png", 400, 100), Banner); err == nil {
		t.Error("tiny banner accepted")
	}
	if _, err := CheckImage(writePNG(t, d, "square.png", 1200, 1200), Banner); err == nil || !strings.Contains(err.Error(), "wide") {
		t.Errorf("square banner accepted or wrong message: %v", err)
	}
	if _, err := CheckImage(writePNG(t, d, "shot.png", 1280, 720), Screenshot); err != nil {
		t.Errorf("good screenshot rejected: %v", err)
	}
	// JPEG works too.
	var jb bytes.Buffer
	if err := jpeg.Encode(&jb, image.NewRGBA(image.Rect(0, 0, 800, 450)), nil); err != nil {
		t.Fatal(err)
	}
	jp := filepath.Join(d, "a.jpg")
	os.WriteFile(jp, jb.Bytes(), 0o644)
	if _, err := CheckImage(jp, Screenshot); err != nil {
		t.Errorf("jpeg rejected: %v", err)
	}
}

func TestImageRejectsByContentNotName(t *testing.T) {
	d := t.TempDir()
	cases := map[string]string{
		"svg-as-png.png":  `<svg xmlns="http://www.w3.org/2000/svg" width="1000" height="500"><script>alert(1)</script></svg>`,
		"gif-as-png.png":  "GIF89a\x01\x00\x01\x00\x00\x00\x00;",
		"html-as-jpg.jpg": "<html><body>hi</body></html>",
		"truncated.png":   "\x89PNG\r\n\x1a\n\x00\x00",
	}
	for name, body := range cases {
		p := filepath.Join(d, name)
		os.WriteFile(p, []byte(body), 0o644)
		if _, err := CheckImage(p, Screenshot); err == nil {
			t.Errorf("%s was accepted", name)
		}
	}
	if _, err := CheckImage(filepath.Join(d, "missing.png"), Screenshot); err == nil {
		t.Error("missing file accepted")
	}
}

func TestOversizeAndVideo(t *testing.T) {
	d := t.TempDir()
	big := filepath.Join(d, "big.png")
	f, _ := os.Create(big)
	f.Write([]byte("\x89PNG\r\n\x1a\n"))
	f.Truncate(5 * MiB)
	f.Close()
	if _, err := CheckImage(big, Screenshot); err == nil || !strings.Contains(err.Error(), "limit") {
		t.Errorf("oversize image accepted: %v", err)
	}
	mp4 := filepath.Join(d, "t.mp4")
	os.WriteFile(mp4, append([]byte("\x00\x00\x00\x18ftypisom"), make([]byte, 100)...), 0o644)
	webm := filepath.Join(d, "t.webm")
	os.WriteFile(webm, append([]byte{0x1A, 0x45, 0xDF, 0xA3}, make([]byte, 100)...), 0o644)
	bad := filepath.Join(d, "t.mov.mp4")
	os.WriteFile(bad, []byte("not a video at all"), 0o644)
	for _, p := range []string{mp4, webm} {
		if _, err := CheckVideo(p); err != nil {
			t.Errorf("%s rejected: %v", p, err)
		}
	}
	if _, err := CheckVideo(bad); err == nil {
		t.Error("fake video accepted")
	}
	huge := filepath.Join(d, "huge.mp4")
	hf, _ := os.Create(huge)
	hf.Write([]byte("\x00\x00\x00\x18ftypisom"))
	hf.Truncate(31 * MiB)
	hf.Close()
	if _, err := CheckVideo(huge); err == nil {
		t.Error("31 MiB video accepted")
	}
}
