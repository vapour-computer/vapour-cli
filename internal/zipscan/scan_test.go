package zipscan

import (
	"archive/zip"
	"bytes"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"hash/crc32"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"
)

const mb = 1 << 20

// zentry is one entry of an archive built by makeZip; mode 0 means a plain
// file with the default mode. raw, when set, bypasses the stdlib writer and
// hand-sizes the header instead (bomb/lie archives).
type zentry struct {
	name    string
	content []byte
	mode    os.FileMode
	dir     bool
	raw     *rawSpec
}

type rawSpec struct {
	name     string
	declared uint64
	body     []byte
	method   uint16 // stored when zero; 99 is the stand-in for an unsupported algorithm
}

func makeZip(t *testing.T, entries []zentry) []byte {
	t.Helper()
	var buf bytes.Buffer
	w := zip.NewWriter(&buf)
	for _, e := range entries {
		if e.raw != nil {
			addRaw(t, w, *e.raw)
			continue
		}
		hdr := &zip.FileHeader{Name: e.name, Method: zip.Deflate}
		switch {
		case e.dir:
			hdr.SetMode(os.ModeDir | 0755)
		case e.mode != 0:
			hdr.SetMode(e.mode)
		}
		f, err := w.CreateHeader(hdr)
		if err != nil {
			t.Fatalf("create entry %q: %v", e.name, err)
		}
		if _, err := f.Write(e.content); err != nil {
			t.Fatalf("write entry %q: %v", e.name, err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// addRaw writes a precompressed raw entry with hand-set sizes, so bomb or
// lying archives can be built without materializing real uncompressed data.
func addRaw(t *testing.T, w *zip.Writer, spec rawSpec) {
	t.Helper()
	method := spec.method
	if method == 0 {
		method = zip.Store
	}
	hdr := &zip.FileHeader{
		Name:               spec.name,
		Method:             method,
		UncompressedSize64: spec.declared,
		CompressedSize64:   uint64(len(spec.body)),
	}
	hdr.CRC32 = crc32.ChecksumIEEE(spec.body)
	f, err := w.CreateRaw(hdr)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.Write(spec.body); err != nil {
		t.Fatal(err)
	}
}

func scanMem(t *testing.T, data []byte, lim Limits) *Report {
	t.Helper()
	r, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("opening built archive: %v", err)
	}
	rep := Scan(r, lim)
	assertReportSorted(t, rep)
	return rep
}

func codesOf(rep *Report) []string {
	codes := make([]string, 0, len(rep.Findings))
	for _, f := range rep.Findings {
		codes = append(codes, f.Code)
	}
	sort.Strings(codes)
	return codes
}

// assertReportSorted enforces the documented report order (errors first, then
// Path, then Code) on every scanned archive, so the rule is tested by
// construction rather than by one dedicated unit test.
func assertReportSorted(t *testing.T, rep *Report) {
	t.Helper()
	for i := 1; i < len(rep.Findings); i++ {
		a, b := rep.Findings[i-1], rep.Findings[i]
		if a.Severity != SeverityError && b.Severity == SeverityError {
			t.Fatalf("warning %q (%s) sorted before error %q (%s)", a.Code, a.Path, b.Code, b.Path)
		}
		if a.Severity != b.Severity {
			continue // a is the error of the pair, handled above
		}
		if a.Path > b.Path {
			t.Fatalf("finding %q (%s) sorted before %q (%s): wrong path order", a.Code, a.Path, b.Code, b.Path)
		}
		if a.Path == b.Path && a.Code > b.Code {
			t.Fatalf("findings for %q out of order: %q before %q", a.Path, a.Code, b.Code)
		}
	}
}

// peSample builds a 0x60-byte DOS/PE header: "MZ" at 0 and a PE signature
// four bytes past the offset stored at 0x3c.
func peSample() []byte {
	b := make([]byte, 0x60)
	b[0], b[1] = 'M', 'Z'
	binary.LittleEndian.PutUint32(b[0x3c:0x40], 0x40)
	copy(b[0x40:], []byte{'P', 'E', 0, 0})
	return b
}

func elfSample() []byte {
	return append([]byte{0x7f, 'E', 'L', 'F', 0x02, 0x01, 0x01}, make([]byte, 8)...)
}

func machOSample(magic []byte) []byte {
	return append(append([]byte{}, magic...), make([]byte, 8)...)
}

// eicarTestString builds the 68-byte EICAR test file from split parts, so the
// test binary itself never holds the literal signature.
func eicarTestString() string {
	head := "X5O!P%@AP[4\\PZX54(P^)7CC)7}$"
	mid := "EICAR-STANDARD-ANTIVIRUS-TEST-FILE!"
	tail := "$H+H*"
	return head + mid + tail
}

// mixture used twice below: 1000 bytes with the EICAR string starting at
// offset 100, inside and outside a small ScanBytesPerFile window.
// lateEicar returns 1000 bytes with the EICAR string starting at offset 100,
// inside and outside a small ScanBytesPerFile window depending on the limit.
func lateEicar() []byte {
	b := make([]byte, 1000)
	for i := range b {
		b[i] = '0'
	}
	copy(b[100:], eicarTestString())
	return b
}

// noisePayload builds deterministic, practically incompressible bytes, so
// big-file tests do not trip the zip_bomb_ratio check.
func noisePayload(n int, seed uint32) []byte {
	b := make([]byte, n)
	x := seed*2654435761 + 12345
	for i := range b {
		x = x*1664525 + 1013904223
		b[i] = byte(x >> 24)
	}
	return b
}

func manyNativeExtFiles() []zentry {
	var files []zentry
	for _, ext := range []string{
		"exe", "dll", "so", "dylib", "msi", "scr", "bat", "cmd", "ps1", "vbs", "jar", "apk",
	} {
		files = append(files, zentry{name: "thing" + "." + ext, content: []byte("d")})
	}
	return files
}

func hoveringCodes(n int, code string) string {
	list := make([]string, n)
	for i := range list {
		list[i] = code
	}
	return strings.Join(list, ",")
}

// table-driven: every archive is built in memory and scanned with real limits.
func TestScanTable(t *testing.T) {
	cases := []struct {
		name  string
		lim   Limits // zero fields fall back to DefaultLimits
		files []zentry
		want  string // sorted, comma-joined codes; "" expects none
	}{
		{
			name: "clean web bundle",
			files: []zentry{
				{name: "index.html", content: []byte(
					`<html><head><script src="https://cdn.example.org/app.js"></script></head>` +
						`<body><script src="app.js"></script><p>hi</p></body></html>`)},
				{name: "assets/game.wasm", content: []byte{0x00, 'a', 's', 'm', 0x01, 0x00, 0x00, 0x00}},
				{name: "assets/img.png", content: append([]byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'}, make([]byte, 64)...)},
				{name: "app.min.js", content: []byte(strings.Repeat("x+1;", 75000) + "String.fromCharCode(7)")},
				{name: "engine.js", content: []byte("eval(\"1+2\");\n")},
				{name: "fonts/style.br", content: []byte("brotli-does-not-decompress-here")},
				{name: "data/save.tar.gz", content: []byte("tar-gz-is-normal")},
				{name: "assets/", dir: true},
			},
			want: "",
		},
		{
			name:  "too many entries",
			lim:   Limits{MaxEntries: 2},
			files: []zentry{{name: "a.txt", content: []byte("x")}, {name: "b.txt", content: []byte("x")}, {name: "c.txt", content: []byte("x")}},
			want:  "too_many_entries",
		},
		{
			name: "zip bomb total",
			lim:  Limits{MaxTotalUncompressed: 1 << 20},
			files: []zentry{
				{name: "a.bin", content: make([]byte, 600*1024)},
				{name: "b.bin", content: make([]byte, 600*1024)},
			},
			// Two plain files: per-file fine, ratio n/a (below 1 MiB), sizes
			// match reality, but the declared total overshoots.
			want: "zip_bomb_total",
		},
		{
			name:  "file too large",
			lim:   Limits{MaxFileUncompressed: 200, MaxTotalUncompressed: 1 << 30},
			files: []zentry{{name: "big.bin", content: []byte(strings.Repeat("A", 300))}},
			want:  "file_too_large",
		},
		{
			name: "zip bomb ratio",
			files: []zentry{
				// 2 MiB of zeros deflate to ~2 KB: a real bomb. The stdlib
				// writer keeps the declared size honest, so only the ratio fires.
				{name: "zeros.bin", content: make([]byte, 2*mb)},
			},
			want: "zip_bomb_ratio",
		},
		{
			name:  "zip bomb ratio with zero compressed size",
			files: []zentry{{raw: &rawSpec{name: "zerocomp.bin", declared: 1 << 20}}},
			// The ratio check rejects the entry, so it is only prefix-read
			// and its undersized body is not re-reported as a size mismatch.
			want: "zip_bomb_ratio",
		},
		{
			name:  "size mismatch declared smaller than actual",
			files: []zentry{{raw: &rawSpec{name: "small.bin", declared: 10, body: bytes.Repeat([]byte("C"), 100)}}},
			want:  "size_mismatch",
		},
		{
			name:  "nested archive",
			files: []zentry{{name: "levels.7z", content: []byte("junk data\n")}},
			want:  "nested_archive",
		},
		{
			name: "path traversal",
			files: []zentry{
				{name: "../evil.txt", content: []byte("x")},
				{name: `win\system32.ini`, content: []byte("x")},
				{name: "ctrl\x00name.txt", content: []byte("x")},
			},
			want: hoveringCodes(3, "path_traversal"),
		},
		{
			name:  "path too long",
			files: []zentry{{name: strings.Repeat("a", 250) + ".txt", content: []byte("x")}},
			want:  "path_too_long",
		},
		{
			name: "path too deep",
			files: []zentry{{
				name:    "a" + strings.Repeat("/a", 20) + "/f.txt",
				content: []byte("x"),
			}},
			want: "path_too_deep",
		},
		{
			name: "duplicate path",
			files: []zentry{
				{name: "Assets/Sprite.png", content: []byte(spritePNG)},
				{name: "assets/sprite.png", content: []byte(spritePNG)},
			},
			want: "duplicate_path",
		},
		{
			name: "symlink and device entries",
			files: []zentry{
				{name: "link.bin", mode: os.ModeSymlink | 0777, content: []byte("target.txt")},
				{name: "chdev.bin", mode: os.ModeDevice | os.ModeCharDevice | 0666},
			},
			want: "symlink,symlink",
		},
		{
			name: "native executable magic under harmless extensions",
			files: []zentry{
				{name: "elf.png", content: elfSample()},
				{name: "pe.png", content: peSample()},
				{name: "macho.png", content: machOSample([]byte{0xce, 0xfa, 0xed, 0xfe})},
				{name: "fat.png", content: machOSample([]byte{0xca, 0xfe, 0xba, 0xbe})},
			},
			want: hoveringCodes(4, "native_executable"),
		},
		{
			name:  "script shebang",
			files: []zentry{{name: "run.sh", content: []byte("#!/bin/sh\necho hi\n")}},
			want:  "script_shebang",
		},
		{
			name:  "native extension",
			files: manyNativeExtFiles(),
			want:  hoveringCodes(12, "native_extension"),
		},
		{
			name: "double extension",
			files: []zentry{
				{name: "game.png.exe", content: []byte("d")},
				{name: "readme.txt.js", content: []byte("d")},
			},
			want: "double_extension,double_extension,native_extension",
		},
		{
			name:  "eicar test signature",
			files: []zentry{{name: "eicar.bin", content: []byte(eicarTestString())}},
			want:  "test_malware_signature",
		},
		{
			name:  "crypto miner text",
			files: []zentry{{name: "skid.js", content: []byte("const pool = \"stratum+tcp://pool.example.com:3333\"; // CoinHive Cryptonight\n")}},
			want:  "crypto_miner",
		},
		{
			name: "obfuscated scripts",
			files: []zentry{
				{name: "evil.js", content: []byte("eval(atob('aGk='));\n")},
				{name: "noise.js", content: []byte(strings.Repeat("String.fromCharCode(65) ", 20))},
				{name: "longline.js", content: []byte(strings.Repeat("a;", 100000) + "eval(x);")},
				{name: "trap.htm", content: []byte("document.write(unescape('%41'));\n")},
			},
			want: hoveringCodes(4, "obfuscated_script"),
		},
		{
			name: "insecure external scripts",
			files: []zentry{
				{name: "shop.html", content: []byte(`<div><script src="http://tracker.example.org/t.js"></script></div>`)},
				{name: "boot.htm", content: []byte(`<html><body><script src="//192.0.2.44/boot.js"></script></body></html>`)},
			},
			want: hoveringCodes(2, "external_script_insecure"),
		},
		{
			name: "content window hides EICAR string",
			lim:  Limits{ScanBytesPerFile: 64},
			// EICAR starts at offset 100, past the 64-byte check window.
			files: []zentry{{name: "late.bin", content: lateEicar()}},
			want:  "",
		},
		{
			name:  "content window sees EICAR string with default limits",
			files: []zentry{{name: "late.bin", content: lateEicar()}},
			want:  "test_malware_signature",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			rep := scanMem(t, makeZip(t, c.files), c.lim)
			got := strings.Join(codesOf(rep), ",")
			if got != c.want {
				t.Errorf("codes = %q, want %q\nreport: %s", got, c.want, findingsDump(rep))
			}
			if c.want == "" && rep.Failed() {
				t.Errorf("clean archive reported as failed: %s", findingsDump(rep))
			}
		})
	}
}

func findingsDump(rep *Report) string {
	var sb strings.Builder
	for _, f := range rep.Findings {
		fmt.Fprintf(&sb, "- [%s] %s %q: %s\n", f.Severity, f.Code, f.Path, f.Message)
	}
	return sb.String()
}

var spritePNG = string(append([]byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'}, make([]byte, 32)...))

func TestEmptyArchive(t *testing.T) {
	var buf bytes.Buffer
	w := zip.NewWriter(&buf)
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	rep := scanMem(t, buf.Bytes(), DefaultLimits())
	if rep.Entries != 0 {
		t.Errorf("Entries = %d, want 0", rep.Entries)
	}
	if got, want := strings.Join(codesOf(rep), ","), "empty_archive"; got != want {
		t.Errorf("codes = %q, want %q", got, want)
	}
	if !rep.Failed() {
		t.Error("empty archive should count as failed")
	}
}

func TestScanFile(t *testing.T) {
	t.Run("scans a real file on disk", func(t *testing.T) {
		data := makeZip(t, []zentry{
			{name: "index.html", content: []byte("<p>hi</p>")},
			{name: "app.js", content: []byte("console.log('hi');")},
			{name: "assets/pack.wasm", content: []byte{0x00, 'a', 's', 'm', 0x01}},
		})
		path := filepath.Join(t.TempDir(), "build.zip")
		if err := os.WriteFile(path, data, 0644); err != nil {
			t.Fatal(err)
		}
		rep, err := ScanFile(path, DefaultLimits())
		if err != nil {
			t.Fatalf("ScanFile: %v", err)
		}
		if rep.Entries != 3 {
			t.Errorf("Entries = %d, want 3", rep.Entries)
		}
		if len(rep.Findings) != 0 || rep.Failed() {
			t.Errorf("clean bundle got findings: %s", findingsDump(rep))
		}
		if rep.UncompressedBytes != 32 { // 9 + 18 + 5: the three file sizes
			t.Errorf("UncompressedBytes = %d, want 32", rep.UncompressedBytes)
		}
		if rep.CompressedBytes <= 0 {
			t.Errorf("CompressedBytes = %d, want > 0", rep.CompressedBytes)
		}
	})

	t.Run("text file is not_a_zip", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "notes.txt")
		if err := os.WriteFile(path, []byte("just some text, no zip here"), 0644); err != nil {
			t.Fatal(err)
		}
		rep, err := ScanFile(path, DefaultLimits())
		if err != nil {
			t.Fatalf("ScanFile returned Go error %v, want a not_a_zip finding", err)
		}
		if got, want := strings.Join(codesOf(rep), ","), "not_a_zip"; got != want {
			t.Errorf("codes = %q, want %q", got, want)
		}
		if !rep.Failed() {
			t.Error("not_a_zip finding should count as failed")
		}
	})

	t.Run("garbage file is not_a_zip", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "garbage.bin")
		if err := os.WriteFile(path, bytes.Repeat([]byte{0xde, 0xad, 0xbe, 0xef}, 64), 0644); err != nil {
			t.Fatal(err)
		}
		rep, err := ScanFile(path, DefaultLimits())
		if err != nil {
			t.Fatalf("ScanFile returned Go error %v, want a not_a_zip finding", err)
		}
		if got, want := strings.Join(codesOf(rep), ","), "not_a_zip"; got != want {
			t.Errorf("codes = %q, want %q", got, want)
		}
	})

	t.Run("missing file is a Go error", func(t *testing.T) {
		_, err := ScanFile(filepath.Join(t.TempDir(), "nope.zip"), DefaultLimits())
		if err == nil {
			t.Fatal("ScanFile succeeded, want I/O error")
		}
		if !os.IsNotExist(err) {
			t.Errorf("error = %v, want a file-not-exist error", err)
		}
	})
}

func TestScanFileMatchesScan(t *testing.T) {
	data := makeZip(t, []zentry{
		{name: "index.html", content: []byte("<p>hi</p>")},
		{name: "evil.js", content: []byte("eval(atob('aGk='));\n")},
		{name: "lib.so", content: elfSample()},
	})
	path := filepath.Join(t.TempDir(), "build.zip")
	if err := os.WriteFile(path, data, 0644); err != nil {
		t.Fatal(err)
	}
	fromFile, err := ScanFile(path, DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	fromMemory := scanMem(t, data, DefaultLimits())
	assertReportSorted(t, fromFile)
	if fromFile.Entries != fromMemory.Entries ||
		fromFile.UncompressedBytes != fromMemory.UncompressedBytes ||
		strings.Join(codesOf(fromFile), ",") != strings.Join(codesOf(fromMemory), ",") {
		t.Errorf("ScanFile and Scan disagree:\nfile: %s\nmem:  %s", findingsDump(fromFile), findingsDump(fromMemory))
	}
}

func TestErrorsBeforeWarnings(t *testing.T) {
	rep := scanMem(t, makeZip(t, []zentry{
		{name: "readme.txt.js", content: []byte("d")},
		{name: "prog.exe", content: []byte("d")},
	}), DefaultLimits())
	if len(rep.Findings) != 2 {
		t.Fatalf("got %d findings, want 2: %s", len(rep.Findings), findingsDump(rep))
	}
	first, second := rep.Findings[0], rep.Findings[1]
	if first.Severity != SeverityError || first.Code != "native_extension" || first.Path != "prog.exe" {
		t.Errorf("first finding = %v %v %v, want error native_extension prog.exe", first.Severity, first.Code, first.Path)
	}
	if second.Severity != SeverityWarn || second.Code != "double_extension" {
		t.Errorf("second finding = %v %v, want warn double_extension", second.Severity, second.Code)
	}
}

func TestReportJSONShape(t *testing.T) {
	rep := &Report{Entries: 2, CompressedBytes: 11, UncompressedBytes: 42, Findings: []Finding{
		{Severity: SeverityWarn, Code: "nested_archive", Path: "a.tar", Message: "msg"},
	}}
	b, err := json.Marshal(rep)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(b, []byte(`"severity":"warn"`)) ||
		!bytes.Contains(b, []byte(`"path":"a.tar"`)) ||
		!bytes.Contains(b, []byte(`"compressed_bytes":11`)) {
		t.Errorf("unexpected JSON: %s", b)
	}
}

// TestScanLargeArchiveIsFast pins the "a 500 MiB archive should scan in a few
// seconds" property with a much smaller stand-in: 50 entries of 1 MiB each.
func TestScanLargeArchiveIsFast(t *testing.T) {
	if testing.Short() {
		t.Skip("perf check")
	}
	files := make([]zentry, 50)
	for i := range files {
		// Quasi-random filler: incompressible, so no ratio findings.
		files[i] = zentry{name: fmt.Sprintf("blob%02d.bin", i), content: noisePayload(mb, uint32(i))}
	}
	start := time.Now()
	rep := scanMem(t, makeZip(t, files), DefaultLimits())
	elapsed := time.Since(start)
	if len(rep.Findings) != 0 {
		t.Errorf("unexpected findings: %s", findingsDump(rep))
	}
	if elapsed > 10*time.Second {
		t.Errorf("scan took %s; should stay well under that", elapsed)
	}
}

func TestScanNeverPanicsOnWeirdNames(t *testing.T) {
	weird := []zentry{
		{name: strings.Repeat("\\", 300), content: []byte("x")},
		{name: "a\x1b[31mb.bin", content: []byte("x")},
		{name: strings.Repeat("../", 29) + "..", content: []byte("x")},
		{name: "...", content: []byte("x")},
		{name: "......txt", content: []byte("x")},
	}
	// Names the stdlib writer refuses ("", ".", "..") go in as raw entries.
	rw := []zentry{
		{raw: &rawSpec{name: "", declared: 1, body: noisePayload(1, 7)}},
		{raw: &rawSpec{name: ".", declared: 1, body: noisePayload(1, 8)}},
		{raw: &rawSpec{name: "..", declared: 1, body: noisePayload(1, 9)}},
	}
	// No assertion beyond not panicking and keeping every report sorted.
	scanMem(t, makeZip(t, append(weird, rw...)), DefaultLimits())
}

// bogusCompression is not a method archive/zip reads, so File.Open fails
// with ErrAlgorithm: the stand-in for unsupported compression or corrupt
// entry data.
const bogusCompression uint16 = 99

// scanAndCount scans an in-memory archive and also returns the number of
// bytes the scanner decompressed, counted through the test hook in scan.go.
func scanAndCount(t *testing.T, data []byte, lim Limits) (*Report, int64) {
	t.Helper()
	var total int64
	called := false
	prev := decompressedHook
	decompressedHook = func(n uint64) { total = int64(n); called = true }
	defer func() { decompressedHook = prev }()

	r, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("opening built archive: %v", err)
	}
	rep := Scan(r, lim)
	assertReportSorted(t, rep)
	if !called {
		t.Error("decompressed hook was not called")
	}
	return rep, total
}

// assertBounded reports the scanner's hard decompression bound,
// MaxTotalUncompressed + ScanBytesPerFile, as an error when total breaches it.
func assertBounded(t *testing.T, lim Limits, total int64) {
	t.Helper()
	if bound := lim.MaxTotalUncompressed + int64(lim.ScanBytesPerFile); total > bound {
		t.Errorf("decompressed %d bytes, above the hard bound of %d", total, bound)
	}
}

func TestDefaultLimitsMatchServer(t *testing.T) {
	lim := DefaultLimits()
	if lim.MaxTotalUncompressed != 1<<30 {
		t.Errorf("MaxTotalUncompressed = %d, want 1<<30 (1 GiB)", lim.MaxTotalUncompressed)
	}
	if lim.MaxFileUncompressed != 512<<20 {
		t.Errorf("MaxFileUncompressed = %d, want 512<<20 (512 MiB)", lim.MaxFileUncompressed)
	}
}

// TestDecompressionBounded pins the behavior for entries that must not get a
// full-size read: already-rejected entries and entries whose declared size
// pushed the running total over the limit.
func TestDecompressionBounded(t *testing.T) {
	t.Run("rejected entry is only prefix-read", func(t *testing.T) {
		// Declares 1 MiB in 4 KiB stored bytes: ratio 256:1 against a 2:1
		// limit. The header is honest, so the full 1 MiB read the entry used
		// to get — and the size_mismatch its undersized body produced — must
		// both be gone; only the content window is decompressed.
		lim := Limits{
			MaxFileUncompressed:  4 << 20,
			MaxTotalUncompressed: 8 << 20,
			MaxRatio:             2,
			ScanBytesPerFile:     2048,
		}
		rep, total := scanAndCount(t, makeZip(t, []zentry{
			{raw: &rawSpec{name: "bomb.bin", declared: 1 << 20, body: noisePayload(4096, 1)}},
		}), lim)
		if got, want := strings.Join(codesOf(rep), ","), "zip_bomb_ratio"; got != want {
			t.Errorf("codes = %q, want %q\nreport: %s", got, want, findingsDump(rep))
		}
		if total != int64(lim.ScanBytesPerFile) {
			t.Errorf("decompressed %d bytes, want exactly the %d-byte window", total, lim.ScanBytesPerFile)
		}
		assertBounded(t, lim, total)
	})

	t.Run("declared total breach stops full-size reads", func(t *testing.T) {
		// a.bin fits the total, so its declared size is fully verified;
		// b.bin pushes the running total over the limit and is only
		// prefix-read despite an honest header; c.bin's lie happens after
		// the breach, so it is not double-reported as size_mismatch.
		lim := Limits{MaxTotalUncompressed: 1500, ScanBytesPerFile: 1024}
		rep, total := scanAndCount(t, makeZip(t, []zentry{
			{raw: &rawSpec{name: "a.bin", declared: 1000, body: noisePayload(1000, 11)}},
			{raw: &rawSpec{name: "b.bin", declared: 1000, body: noisePayload(1000, 12)}},
			{raw: &rawSpec{name: "c.bin", declared: 1000, body: noisePayload(2000, 13)}},
		}), lim)
		if got, want := strings.Join(codesOf(rep), ","), "zip_bomb_total"; got != want {
			t.Errorf("codes = %q, want %q\nreport: %s", got, want, findingsDump(rep))
		}
		if want := int64(1000 + 1000 + 24); total != want {
			t.Errorf("decompressed %d bytes, want %d (a verified, b and c prefix only)", total, want)
		}
		assertBounded(t, lim, total)
	})

	t.Run("many rejected entries share one window", func(t *testing.T) {
		lim := Limits{
			MaxFileUncompressed:  32 << 10,
			MaxTotalUncompressed: 128 << 10,
			ScanBytesPerFile:     1024,
		}
		entries := make([]zentry, 5)
		for i := range entries {
			entries[i] = zentry{raw: &rawSpec{
				name:     fmt.Sprintf("blob%02d.bin", i),
				declared: 64 << 10,
				body:     noisePayload(2048, uint32(i)+7),
			}}
		}
		rep, total := scanAndCount(t, makeZip(t, entries), lim)
		want := hoveringCodes(5, "file_too_large") + ",zip_bomb_total"
		if got := strings.Join(codesOf(rep), ","); got != want {
			t.Errorf("codes = %q, want %q\nreport: %s", got, want, findingsDump(rep))
		}
		if total != int64(lim.ScanBytesPerFile) {
			t.Errorf("decompressed %d bytes, want exactly one %d-byte window", total, lim.ScanBytesPerFile)
		}
		assertBounded(t, lim, total)
	})

	t.Run("size_mismatch still reported for normal lying entries", func(t *testing.T) {
		// Below the per-file and total limits: the header lie is checked the
		// full-size, honest way.
		lim := Limits{MaxFileUncompressed: 1 << 20, MaxTotalUncompressed: 2 << 20}
		rep, _ := scanAndCount(t, makeZip(t, []zentry{
			{raw: &rawSpec{name: "small.bin", declared: 10, body: noisePayload(100, 21)}},
		}), lim)
		if got, want := strings.Join(codesOf(rep), ","), "size_mismatch"; got != want {
			t.Errorf("codes = %q, want %q\nreport: %s", got, want, findingsDump(rep))
		}
	})
}

func TestUnreadableEntryReported(t *testing.T) {
	lim := Limits{
		MaxFileUncompressed:  1 << 20,
		MaxTotalUncompressed: 32 << 20,
		MaxRatio:             100000, // keep the byte-count ratio out of the way
		ScanBytesPerFile:     1024,
	}
	// broken.bin fits all limits, so Open fails on the full-size read path;
	// broken2.bin is rejected by file_too_large, so Open fails on the
	// prefix-only path. Both must report entry_unreadable, not size_mismatch.
	rep := scanMem(t, makeZip(t, []zentry{
		{raw: &rawSpec{name: "broken.bin", declared: 42, body: noisePayload(3, 5), method: bogusCompression}},
		{raw: &rawSpec{name: "broken2.bin", declared: 3 << 20, body: noisePayload(1024, 6), method: bogusCompression}},
		{name: "ok.txt", content: []byte("fine")},
	}), lim)
	if got, want := strings.Join(codesOf(rep), ","), "entry_unreadable,entry_unreadable,file_too_large"; got != want {
		t.Errorf("codes = %q, want %q\nreport: %s", got, want, findingsDump(rep))
	}
	const wantMsg = "Entry cannot be opened: unsupported compression or corrupt data."
	for _, f := range rep.Findings {
		if f.Code != "entry_unreadable" {
			continue
		}
		if f.Severity != SeverityError || f.Message != wantMsg ||
			(f.Path != "broken.bin" && f.Path != "broken2.bin") {
			t.Errorf("wrong entry_unreadable finding: %v", f)
		}
	}
}
