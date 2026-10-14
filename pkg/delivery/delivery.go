// Package delivery implements the experimental identity-encoded v1 delivery protocol.
package delivery

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const MaxFile int64 = 4 << 30
const MaxGame int64 = 50 << 30
const MaxChunk = 8 << 20

type Chunk struct {
	Hash string `json:"hash"`
	Size int64  `json:"size"`
}
type File struct {
	Path   string  `json:"path"`
	Size   int64   `json:"size"`
	Boot   bool    `json:"boot"`
	Chunks []Chunk `json:"chunks"`
}
type Manifest struct {
	Schema  int    `json:"schema"`
	Version string `json:"version"`
	Files   []File `json:"files"`
}
type Missing struct {
	Hash   string `json:"hash"`
	URL    string `json:"url"`
	Offset int64  `json:"offset"`
}
type Session struct {
	ID      string    `json:"id"`
	Missing []Missing `json:"missing"`
}

// Split uses a deterministic gear rolling hash, a 1 MiB minimum, ~4 MiB
// boundary mask and an 8 MiB maximum. Memory is bounded by one chunk.
func Split(r io.Reader, emit func([]byte) error) error {
	b := bufio.NewReaderSize(r, 64<<10)
	chunk := make([]byte, 0, MaxChunk)
	var hash uint64
	for {
		c, err := b.ReadByte()
		if err == io.EOF {
			if len(chunk) > 0 {
				return emit(chunk)
			}
			return nil
		}
		if err != nil {
			return err
		}
		// SplitMix64-derived gear table value; deterministic across platforms.
		x := uint64(c) + 0x9e3779b97f4a7c15
		x = (x ^ (x >> 30)) * 0xbf58476d1ce4e5b9
		x = (x ^ (x >> 27)) * 0x94d049bb133111eb
		x ^= x >> 31
		hash = (hash << 1) + x
		chunk = append(chunk, c)
		if len(chunk) == MaxChunk || (len(chunk) >= 1<<20 && hash&((1<<22)-1) == 0) {
			if err := emit(chunk); err != nil {
				return err
			}
			chunk = chunk[:0]
			hash = 0
		}
	}
}

// Pack spools unique chunks to cache; callers own the cache's lifetime. Symlinks
// and special files are rejected so exports cannot escape their build directory.
func Pack(root, cache, version string, boot []string) (Manifest, error) {
	m := Manifest{Schema: 1, Version: version}
	var total int64
	if err := os.MkdirAll(cache, 0700); err != nil {
		return m, err
	}
	verified := map[string]bool{}
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, e error) error {
		if e != nil {
			return e
		}
		if d.IsDir() {
			return nil
		}
		if !d.Type().IsRegular() {
			return fmt.Errorf("non-regular export: %s", p)
		}
		st, e := d.Info()
		if e != nil {
			return e
		}
		total += st.Size()
		if st.Size() > MaxFile || total > MaxGame {
			return fmt.Errorf("export exceeds 4 GiB/file or 50 GiB/game quota")
		}
		rel, e := filepath.Rel(root, p)
		if e != nil {
			return e
		}
		f := File{Path: filepath.ToSlash(rel), Size: st.Size(), Chunks: []Chunk{}}
		for _, g := range boot {
			matched, e := filepath.Match(g, f.Path)
			if e != nil {
				return e
			}
			f.Boot = f.Boot || matched
		}
		in, e := os.Open(p)
		if e != nil {
			return e
		}
		defer in.Close()
		opened, e := in.Stat()
		if e != nil {
			return e
		}
		if !opened.Mode().IsRegular() || !os.SameFile(st, opened) {
			return fmt.Errorf("export changed while opening %s", p)
		}
		var actual int64
		e = Split(io.LimitReader(in, st.Size()+1), func(b []byte) error {
			actual += int64(len(b))
			if actual > st.Size() || actual > MaxFile {
				return fmt.Errorf("export changed size while packing %s", p)
			}
			sum := sha256.Sum256(b)
			h := hex.EncodeToString(sum[:])
			f.Chunks = append(f.Chunks, Chunk{h, int64(len(b))})
			dest := filepath.Join(cache, h)
			if verified[h] {
				return nil
			}
			if existing, err := os.Open(dest); err == nil {
				hasher := sha256.New()
				n, err := io.Copy(hasher, io.LimitReader(existing, MaxChunk+1))
				existing.Close()
				if err == nil && n == int64(len(b)) && hex.EncodeToString(hasher.Sum(nil)) == h {
					verified[h] = true
					return nil
				}
			}
			tmp, err := os.CreateTemp(cache, ".chunk-*")
			if err != nil {
				return err
			}
			defer os.Remove(tmp.Name())
			if _, err := tmp.Write(b); err != nil {
				tmp.Close()
				return err
			}
			if err := tmp.Close(); err != nil {
				return err
			}
			if err := os.Rename(tmp.Name(), dest); err != nil {
				return err
			}
			verified[h] = true
			return nil
		})
		if e != nil {
			return e
		}
		if actual != st.Size() {
			return fmt.Errorf("export changed size while packing %s", p)
		}
		m.Files = append(m.Files, f)
		return nil
	})
	return m, err
}

func JSON(ctx context.Context, c *http.Client, method, url, token string, in, out any) error {
	var body io.Reader
	if in != nil {
		b, e := json.Marshal(in)
		if e != nil {
			return e
		}
		body = bytes.NewReader(b)
	}
	req, e := http.NewRequestWithContext(ctx, method, url, body)
	if e != nil {
		return e
	}
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	res, e := c.Do(req)
	if e != nil {
		return e
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return fmt.Errorf("%s %s: HTTP %d", method, req.URL.Path, res.StatusCode)
	}
	if out != nil {
		return json.NewDecoder(io.LimitReader(res.Body, 8<<20)).Decode(out)
	}
	return nil
}

// Upload sends only missing chunks, with at most four concurrent requests. A
// subsequent invocation resumes at offsets returned by the upload API. Tokens
// go only to the API, never to signed object URLs. Mid-request retry is explicit.
func Upload(ctx context.Context, base, game, token, cache string, m Manifest) (int64, error) {
	c := &http.Client{Timeout: 5 * time.Minute, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	endpoint := strings.TrimRight(base, "/") + "/projects/" + game + "/uploads"
	var s Session
	if e := JSON(ctx, c, "POST", endpoint, token, m, &s); e != nil {
		return 0, e
	}
	sizes := map[string]int64{}
	for _, f := range m.Files {
		for _, ch := range f.Chunks {
			sizes[ch.Hash] = ch.Size
		}
	}
	var mu sync.Mutex
	var sent int64
	var first error
	jobs := make(chan Missing)
	var wg sync.WaitGroup
	for range 4 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for item := range jobs {
				e := func() error {
					size, ok := sizes[item.Hash]
					if !ok || item.Offset < 0 || item.Offset > size {
						return fmt.Errorf("invalid missing chunk or resume offset")
					}
					f, e := os.Open(filepath.Join(cache, item.Hash))
					if e != nil {
						return e
					}
					defer f.Close()
					if _, e = f.Seek(item.Offset, 0); e != nil {
						return e
					}
					req, e := http.NewRequestWithContext(ctx, "PUT", item.URL, io.LimitReader(f, size-item.Offset))
					if e != nil {
						return e
					}
					req.ContentLength = size - item.Offset
					req.Header.Set("Upload-Offset", fmt.Sprint(item.Offset))
					res, e := c.Do(req)
					if e != nil {
						return e
					}
					defer res.Body.Close()
					if res.StatusCode < 200 || res.StatusCode >= 300 {
						return fmt.Errorf("chunk PUT: HTTP %d", res.StatusCode)
					}
					mu.Lock()
					sent += size - item.Offset
					mu.Unlock()
					return nil
				}()
				if e != nil {
					mu.Lock()
					if first == nil {
						first = e
					}
					mu.Unlock()
				}
			}
		}()
	}
	for _, item := range s.Missing {
		jobs <- item
	}
	close(jobs)
	wg.Wait()
	if first != nil {
		return sent, first
	}
	return sent, JSON(ctx, c, "POST", endpoint+"/"+s.ID+"/finalize", token, nil, nil)
}
