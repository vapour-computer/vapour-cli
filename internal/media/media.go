// Package media checks store images and trailers before upload, using the same rules as the API.
// Types are detected from file content, never from the file name.
package media

import (
	"fmt"
	"image"
	_ "image/jpeg" // register decoders for image.DecodeConfig
	_ "image/png"
	"io"
	"os"

	_ "golang.org/x/image/webp"
)

const MiB = 1 << 20

type Spec struct {
	Name           string
	MaxBytes       int64
	MinW, MinH     int
	MaxW, MaxH     int
	MinAspectRatio float64 // width/height, 0 = no minimum
}

var (
	Banner     = Spec{Name: "banner", MaxBytes: 4 * MiB, MinW: 960, MinH: 300, MaxW: 8192, MaxH: 4096, MinAspectRatio: 2}
	Cover      = Spec{Name: "cover", MaxBytes: 2 * MiB, MinW: 460, MinH: 215, MaxW: 4096, MaxH: 4096}
	Screenshot = Spec{Name: "screenshot", MaxBytes: 4 * MiB, MinW: 640, MinH: 360, MaxW: 8192, MaxH: 8192}
)

const (
	MaxVideoBytes  = 30 * MiB
	MaxScreenshots = 8
	// MaxRequestBytes is Cloud Run's request limit; one upload request must stay below it.
	MaxRequestBytes = 32 * MiB
)

// Kind returns "png", "jpeg", "webp", "mp4" or "webm" from the first bytes, or "" when unknown.
func Kind(head []byte) string {
	switch {
	case len(head) >= 8 && string(head[:8]) == "\x89PNG\r\n\x1a\n":
		return "png"
	case len(head) >= 3 && head[0] == 0xFF && head[1] == 0xD8 && head[2] == 0xFF:
		return "jpeg"
	case len(head) >= 12 && string(head[:4]) == "RIFF" && string(head[8:12]) == "WEBP":
		return "webp"
	case len(head) >= 8 && string(head[4:8]) == "ftyp":
		return "mp4"
	case len(head) >= 4 && head[0] == 0x1A && head[1] == 0x45 && head[2] == 0xDF && head[3] == 0xA3:
		return "webm"
	}
	return ""
}

func open(path string) (*os.File, int64, []byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, 0, nil, err
	}
	st, err := f.Stat()
	if err != nil {
		f.Close()
		return nil, 0, nil, err
	}
	if !st.Mode().IsRegular() {
		f.Close()
		return nil, 0, nil, fmt.Errorf("%s is not a regular file", path)
	}
	head := make([]byte, 16)
	n, _ := io.ReadFull(f, head)
	return f, st.Size(), head[:n], nil
}

// CheckImage validates one image against spec and returns its size in bytes.
func CheckImage(path string, spec Spec) (int64, error) {
	f, size, head, err := open(path)
	if err != nil {
		return 0, fmt.Errorf("%s: %w", spec.Name, err)
	}
	defer f.Close()
	if size > spec.MaxBytes {
		return 0, fmt.Errorf("%s %s is %.1f MiB; the limit is %d MiB", spec.Name, path, float64(size)/MiB, spec.MaxBytes/MiB)
	}
	switch Kind(head) {
	case "png", "jpeg", "webp":
	default:
		return 0, fmt.Errorf("%s %s must be a PNG, JPEG or WebP image (SVG, GIF and other formats are not accepted)", spec.Name, path)
	}
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return 0, err
	}
	cfg, _, err := image.DecodeConfig(f)
	if err != nil {
		return 0, fmt.Errorf("%s %s is not a readable image: %w", spec.Name, path, err)
	}
	if cfg.Width < spec.MinW || cfg.Height < spec.MinH {
		return 0, fmt.Errorf("%s %s is %dx%d; it must be at least %dx%d", spec.Name, path, cfg.Width, cfg.Height, spec.MinW, spec.MinH)
	}
	if cfg.Width > spec.MaxW || cfg.Height > spec.MaxH {
		return 0, fmt.Errorf("%s %s is %dx%d; the maximum is %dx%d", spec.Name, path, cfg.Width, cfg.Height, spec.MaxW, spec.MaxH)
	}
	if spec.MinAspectRatio > 0 && float64(cfg.Width)/float64(cfg.Height) < spec.MinAspectRatio {
		return 0, fmt.Errorf("%s %s is %dx%d; it must be wide (at least %.0f:1)", spec.Name, path, cfg.Width, cfg.Height, spec.MinAspectRatio)
	}
	return size, nil
}

// CheckVideo validates a trailer (MP4 or WebM, at most 30 MiB) and returns its size in bytes.
func CheckVideo(path string) (int64, error) {
	f, size, head, err := open(path)
	if err != nil {
		return 0, fmt.Errorf("video: %w", err)
	}
	f.Close()
	if size > MaxVideoBytes {
		return 0, fmt.Errorf("video %s is %.1f MiB; the limit is %d MiB (shorten or compress the trailer)", path, float64(size)/MiB, MaxVideoBytes/MiB)
	}
	if k := Kind(head); k != "mp4" && k != "webm" {
		return 0, fmt.Errorf("video %s must be an MP4 or WebM file", path)
	}
	return size, nil
}
