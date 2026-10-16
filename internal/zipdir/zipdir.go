// Package zipdir checks and zips a web export folder for upload.
package zipdir

import (
	"archive/zip"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// Native binaries cannot run in a browser; catch the common mistake of zipping a desktop build.
var nativeExt = map[string]bool{".exe": true, ".dll": true, ".so": true, ".dylib": true, ".app": true, ".msi": true}

var skipNames = map[string]bool{".git": true, ".DS_Store": true, "Thumbs.db": true}

var storeCompressedExt = map[string]bool{
	".br": true, ".gz": true, ".zip": true, ".png": true, ".jpg": true, ".jpeg": true,
	".webp": true, ".mp3": true, ".ogg": true, ".mp4": true, ".webm": true,
	".woff": true, ".woff2": true,
}

// Check verifies dir looks like a web export: it has an index.html and no native binaries.
func Check(dir string) error {
	st, err := os.Stat(dir)
	if err != nil {
		return fmt.Errorf("build folder %q not found: export your game for Web first", dir)
	}
	if !st.IsDir() {
		return fmt.Errorf("build path %q is not a folder", dir)
	}
	if _, err := os.Stat(filepath.Join(dir, "index.html")); err != nil {
		return fmt.Errorf("%q has no index.html: export your game for Web (HTML5) and point build.dir at the export folder", dir)
	}
	var native []string
	err = filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if skipNames[d.Name()] && d.IsDir() {
			return filepath.SkipDir
		}
		if nativeExt[strings.ToLower(filepath.Ext(d.Name()))] {
			rel, _ := filepath.Rel(dir, p)
			native = append(native, rel)
		}
		return nil
	})
	if err != nil {
		return err
	}
	if len(native) > 0 {
		return fmt.Errorf("found native binaries (%s): browsers cannot run these; upload the Web export only", strings.Join(native, ", "))
	}
	return nil
}

// Options changes how Zip packs a folder.
type Options struct {
	// PointerLock adds the pointer-lock script to the root index.html inside the zip.
	// Files on disk are never modified.
	PointerLock bool
	// PointerLockPause sends Escape to the game when the browser drops pointer lock.
	PointerLockPause bool
	// PointerLockEntry selects which HTML file gets the pointer-lock script.
	// Empty means index.html.
	PointerLockEntry string
}

// Zip writes every file under dir to w as a zip with forward-slash paths. It returns the number of files.
func Zip(dir string, w io.Writer) (int, error) {
	return ZipWith(dir, w, Options{})
}

// ZipWith is Zip with options.
func ZipWith(dir string, w io.Writer, opts Options) (int, error) {
	zw := zip.NewWriter(w)
	n := 0
	injectEntry := filepath.ToSlash(filepath.Clean(opts.PointerLockEntry))
	if injectEntry == "." || injectEntry == "" {
		injectEntry = "index.html"
	}
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if skipNames[d.Name()] {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if d.IsDir() {
			return nil
		}
		if !d.Type().IsRegular() {
			return fmt.Errorf("%s is not a regular file (symlinks are not supported)", p)
		}
		rel, err := filepath.Rel(dir, p)
		if err != nil {
			return err
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		hName := filepath.ToSlash(rel)
		f, err := os.Open(p)
		if err != nil {
			return err
		}
		store, err := shouldStore(rel, f)
		if err != nil {
			_ = f.Close()
			return err
		}
		h, err := zip.FileInfoHeader(info)
		if err != nil {
			_ = f.Close()
			return err
		}
		h.Name = hName
		if store {
			h.Method = zip.Store
		} else {
			h.Method = zip.Deflate
		}
		if opts.PointerLock && h.Name == injectEntry {
			raw, err := io.ReadAll(f)
			if err != nil {
				_ = f.Close()
				return err
			}
			_ = f.Close()
			// The size changes, so let the writer compute it.
			h.UncompressedSize64, h.CompressedSize64, h.CRC32 = 0, 0, 0
			fw, err := zw.CreateHeader(h)
			if err != nil {
				return err
			}
			if _, err := fw.Write(InjectPointerLock(raw, opts.PointerLockPause)); err != nil {
				return err
			}
			n++
			return nil
		}
		fw, err := zw.CreateHeader(h)
		if err != nil {
			_ = f.Close()
			return err
		}
		if _, err := f.Seek(0, io.SeekStart); err != nil {
			_ = f.Close()
			return err
		}
		if _, err := io.Copy(fw, f); err != nil {
			_ = f.Close()
			return err
		}
		_ = f.Close()
		n++
		return nil
	})
	if err != nil {
		return n, err
	}
	if n == 0 {
		return 0, errors.New("build folder is empty")
	}
	return n, zw.Close()
}

func shouldStore(rel string, f *os.File) (bool, error) {
	lower := strings.ToLower(rel)
	if storeCompressedExt[strings.ToLower(filepath.Ext(rel))] {
		return true, nil
	}
	if !strings.HasSuffix(lower, ".unityweb") {
		return false, nil
	}
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return false, err
	}
	buf := make([]byte, 512)
	n, err := f.Read(buf)
	if err != nil && err != io.EOF {
		return false, err
	}
	buf = buf[:n]
	if len(buf) >= 2 && buf[0] == 0x1f && buf[1] == 0x8b {
		return true, nil
	}
	if strings.Contains(string(buf), "UnityWeb Compressed Content (brotli)") {
		return true, nil
	}
	return false, nil
}

// IsZip reports whether path starts with the zip magic bytes.
func IsZip(path string) bool {
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()
	b := make([]byte, 4)
	if _, err := io.ReadFull(f, b); err != nil {
		return false
	}
	return string(b) == "PK\x03\x04"
}
