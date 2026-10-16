// Package zipscan statically inspects a build zip before `vapour publish`
// uploads it. It never extracts data to disk, never expands nested archives,
// and reads a bounded prefix of each file for content checks.
package zipscan

import (
	"archive/zip"
	"fmt"
	"io"
	"os"
	"path"
	"sort"
	"strings"
)

// Severity says whether a finding blocks upload or is only shown to the developer.
type Severity string

const (
	SeverityError Severity = "error" // upload must be rejected
	SeverityWarn  Severity = "warn"  // shown to the developer, upload continues
)

// Finding is one problem found in the archive.
type Finding struct {
	Severity Severity `json:"severity"`
	Code     string   `json:"code"`           // stable machine code, e.g. "zip_bomb_ratio"
	Path     string   `json:"path,omitempty"` // entry name inside the zip, if any
	Message  string   `json:"message"`        // one plain sentence for a developer
}

// Limits are the scan thresholds. A zero value for any field means "use the default".
type Limits struct {
	MaxEntries           int
	MaxTotalUncompressed int64
	MaxFileUncompressed  int64
	MaxRatio             int
	MaxPathLen           int
	MaxDepth             int
	ScanBytesPerFile     int64
}

// DefaultLimits returns the limits `vapour publish` uses. The uncompressed
// ceilings match the server: 1 GiB of uncompressed data in total and 512 MiB
// for any one file.
func DefaultLimits() Limits {
	return Limits{
		MaxEntries:           5000,
		MaxTotalUncompressed: 1 << 30,   // 1 GiB
		MaxFileUncompressed:  512 << 20, // 512 MiB
		MaxRatio:             200,
		MaxPathLen:           240,
		MaxDepth:             20,
		ScanBytesPerFile:     4 << 20, // 4 MiB
	}
}

func resolveLimits(l Limits) Limits {
	def := DefaultLimits()
	if l.MaxEntries == 0 {
		l.MaxEntries = def.MaxEntries
	}
	if l.MaxTotalUncompressed == 0 {
		l.MaxTotalUncompressed = def.MaxTotalUncompressed
	}
	if l.MaxFileUncompressed == 0 {
		l.MaxFileUncompressed = def.MaxFileUncompressed
	}
	if l.MaxRatio == 0 {
		l.MaxRatio = def.MaxRatio
	}
	if l.MaxPathLen == 0 {
		l.MaxPathLen = def.MaxPathLen
	}
	if l.MaxDepth == 0 {
		l.MaxDepth = def.MaxDepth
	}
	if l.ScanBytesPerFile == 0 {
		l.ScanBytesPerFile = def.ScanBytesPerFile
	}
	return l
}

// Report is the result of scanning one archive.
type Report struct {
	Entries           int       `json:"entries"`
	CompressedBytes   int64     `json:"compressed_bytes"`
	UncompressedBytes int64     `json:"uncompressed_bytes"`
	Findings          []Finding `json:"findings"`
}

// Failed reports whether any finding has error severity, meaning the upload
// must be rejected.
func (r *Report) Failed() bool {
	for _, f := range r.Findings {
		if f.Severity == SeverityError {
			return true
		}
	}
	return false
}

// ScanFile scans the zip at zipPath. A Go error is returned only for I/O
// failures opening the file; anything that is not a readable zip becomes a
// "not_a_zip" finding instead.
func ScanFile(zipPath string, lim Limits) (*Report, error) {
	f, err := os.Open(zipPath)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return nil, err
	}
	zr, err := zip.NewReader(f, info.Size())
	if err != nil {
		return &Report{Findings: []Finding{{
			Severity: SeverityError,
			Code:     "not_a_zip",
			Message:  "File is not a readable zip archive.",
		}}}, nil
	}
	return Scan(zr, lim), nil
}

// Scan scans an already opened archive; size is the zip file size.
func Scan(r *zip.Reader, lim Limits) *Report {
	lim = resolveLimits(lim)
	rep := &Report{Entries: len(r.File), Findings: []Finding{}}
	add := func(f Finding) { rep.Findings = append(rep.Findings, f) }

	if len(r.File) == 0 {
		add(Finding{
			Severity: SeverityError,
			Code:     "empty_archive",
			Message:  "Archive contains no entries.",
		})
		sortFindings(rep.Findings)
		return rep
	}
	if len(r.File) > lim.MaxEntries {
		add(Finding{
			Severity: SeverityError,
			Code:     "too_many_entries",
			Message: fmt.Sprintf("Archive contains %d entries, above the maximum of %d.",
				len(r.File), lim.MaxEntries),
		})
	}

	s := &scanner{
		lim:        lim,
		buf:        make([]byte, lim.ScanBytesPerFile),
		seen:       make(map[string]bool),
		add:        add,
		fullLeft:   lim.MaxTotalUncompressed,
		prefixLeft: lim.ScanBytesPerFile,
	}

	var totalUncompressed, totalCompressed uint64
	var declaredTotal uint64 // running sum of declared uncompressed sizes
	for _, f := range r.File {
		totalUncompressed += f.UncompressedSize64
		totalCompressed += f.CompressedSize64
		declaredTotal += f.UncompressedSize64

		s.checkPath(f.Name)
		s.checkName(f.Name)

		mode := f.FileInfo().Mode()
		switch {
		case mode.IsDir():
			continue
		case !mode.IsRegular():
			add(Finding{
				Severity: SeverityError,
				Code:     "symlink",
				Path:     f.Name,
				Message:  "Entry is a symlink, device, pipe or socket instead of a regular file.",
			})
			continue
		}

		un := f.UncompressedSize64
		comp := f.CompressedSize64
		rejected := false
		if un > uint64(lim.MaxFileUncompressed) {
			add(Finding{
				Severity: SeverityError,
				Code:     "file_too_large",
				Path:     f.Name,
				Message: fmt.Sprintf("File declares %d uncompressed bytes, above the per-file maximum of %d bytes.",
					un, uint64(lim.MaxFileUncompressed)),
			})
			rejected = true
		}
		if reason := bombRatioReason(un, comp, lim.MaxRatio); reason != "" {
			add(Finding{
				Severity: SeverityError,
				Code:     "zip_bomb_ratio",
				Path:     f.Name,
				Message:  reason,
			})
			rejected = true
		}

		// Entries that are already rejected and entries whose declared size
		// pushed the running total over the limit get no full-size read: only
		// the content window is decompressed, so the scanner never expands
		// more than MaxTotalUncompressed (all full-size reads together) plus
		// ScanBytesPerFile (all window reads together) bytes in total.
		full := !rejected && declaredTotal <= uint64(lim.MaxTotalUncompressed)
		if !full {
			content, err := s.readPrefix(f)
			if err != nil {
				add(unreadableFinding(f.Name))
				continue
			}
			s.checkContent(f.Name, content)
			continue
		}

		content, counted, verified, err := s.readForSize(f)
		if err != nil {
			add(unreadableFinding(f.Name))
			continue
		}
		if verified && counted != un {
			add(Finding{
				Severity: SeverityError,
				Code:     "size_mismatch",
				Path:     f.Name,
				Message: fmt.Sprintf("File declares %d uncompressed bytes but decompressed to %d bytes within the safety cap.",
					un, counted),
			})
		}
		s.checkContent(f.Name, content)
	}

	if decompressedHook != nil {
		decompressedHook(s.decompressed)
	}

	if totalUncompressed > uint64(lim.MaxTotalUncompressed) {
		add(Finding{
			Severity: SeverityError,
			Code:     "zip_bomb_total",
			Message: fmt.Sprintf("Archive declares %d uncompressed bytes in total, above the maximum of %d bytes.",
				totalUncompressed, uint64(lim.MaxTotalUncompressed)),
		})
	}
	rep.CompressedBytes = int64(totalCompressed)
	rep.UncompressedBytes = int64(totalUncompressed)
	sortFindings(rep.Findings)
	return rep
}

// scanner holds per-scan state that is reused across entries to keep memory flat.
type scanner struct {
	lim          Limits
	buf          []byte // content window buffer, ScanBytesPerFile bytes
	seen         map[string]bool
	add          func(Finding)
	fullLeft     int64  // decompressed bytes full-size reads may still consume
	prefixLeft   int64  // decompressed bytes window reads may still consume
	decompressed uint64 // bytes actually decompressed across the whole scan
}

// unreadableFinding is reported when f.Open() fails for an entry: unsupported
// compression or corrupt local data, which is a distinct problem from a
// lying header.
func unreadableFinding(name string) Finding {
	return Finding{
		Severity: SeverityError,
		Code:     "entry_unreadable",
		Path:     name,
		Message:  "Entry cannot be opened: unsupported compression or corrupt data.",
	}
}

// decompressedHook, when non-nil, receives the total number of bytes one Scan
// call decompressed. Tests set it to pin the scanner's decompression bound;
// production code leaves it nil.
var decompressedHook func(bytesDecompressed uint64)

// minRatioBytes is the uncompressed size from which the ratio rule applies.
const minRatioBytes = 1 << 20

// bombRatioReason returns the zip_bomb_ratio finding message, or "" if the
// entry is fine. The ratio is evaluated exactly, without floating point or
// overflow: flagged when uncompressed/compressed > MaxRatio (real ratio), or
// when compressed is zero and uncompressed is not.
func bombRatioReason(un, comp uint64, maxRatio int) string {
	if un < minRatioBytes {
		return ""
	}
	if comp == 0 {
		return fmt.Sprintf("File declares %d uncompressed bytes in %d compressed bytes, which looks like a zip bomb.", un, comp)
	}
	q, rem := un/comp, un%comp
	if q > uint64(maxRatio) || (q == uint64(maxRatio) && rem > 0) {
		return fmt.Sprintf("File expands %d bytes from %d compressed bytes (over %d:1), which looks like a zip bomb.",
			un, comp, maxRatio)
	}
	return ""
}

// checkPath emits the path checks for one entry name.
func (s *scanner) checkPath(name string) {
	if reason := traversalReason(name); reason != "" {
		s.add(Finding{
			Severity: SeverityError,
			Code:     "path_traversal",
			Path:     name,
			Message: fmt.Sprintf("Entry path %q %s and could escape the extraction directory.",
				shortName(name), reason),
		})
	}
	if n := len(name); n > s.lim.MaxPathLen {
		s.add(Finding{
			Severity: SeverityError,
			Code:     "path_too_long",
			Path:     name,
			Message: fmt.Sprintf("Entry path is %d characters, above the maximum of %d.",
				n, s.lim.MaxPathLen),
		})
	}
	if depth := segmentCount(name); depth > s.lim.MaxDepth {
		s.add(Finding{
			Severity: SeverityError,
			Code:     "path_too_deep",
			Path:     name,
			Message: fmt.Sprintf("Entry path has %d path segments, above the maximum of %d.",
				depth, s.lim.MaxDepth),
		})
	}
	key := canonicalPath(name)
	if s.seen[key] {
		s.add(Finding{
			Severity: SeverityError,
			Code:     "duplicate_path",
			Path:     name,
			Message: fmt.Sprintf("Two entries unpack to the same name %q after lowercasing and cleaning.",
				shortName(name)),
		})
	}
	s.seen[key] = true
}

// traversalReason describes the first unsafe property of an entry name.
func traversalReason(name string) string {
	if name == "" {
		return ""
	}
	for _, seg := range splitPathSegments(name) {
		if seg == ".." {
			return `contains a ".." segment`
		}
	}
	if strings.HasPrefix(name, "/") || strings.HasPrefix(name, "\\") {
		return "starts at the filesystem root"
	}
	if len(name) >= 2 && name[1] == ':' && isDriveLetter(name[0]) {
		return "carries a Windows drive letter"
	}
	if strings.Contains(name, "\\") {
		return "contains a backslash"
	}
	for i := 0; i < len(name); i++ {
		c := name[i]
		if c == 0 {
			return "contains a NUL byte"
		}
		if c < 0x20 || c == 0x7f {
			return "contains a control character"
		}
	}
	return ""
}

func isDriveLetter(c byte) bool {
	return (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
}

func splitPathSegments(name string) []string {
	return strings.FieldsFunc(name, func(r rune) bool { return r == '/' || r == '\\' })
}

// segmentCount counts non-empty path segments.
func segmentCount(name string) int {
	n := 0
	for _, seg := range splitPathSegments(strings.Trim(name, "/")) {
		if seg != "" {
			n++
		}
	}
	return n
}

// canonicalPath folds a name down to the key used for duplicate detection.
func canonicalPath(name string) string {
	return path.Clean(lowerASCII(strings.ReplaceAll(name, "\\", "/")))
}

// readForSize reads one entry over a single stream: the first
// ScanBytesPerFile bytes are kept for content checks, and the copy continues
// into a fixed-size discard buffer up to declared size + 1 bytes so the
// declared size can be checked. Full-size reads across a whole scan may not
// consume more than MaxTotalUncompressed uncompressed bytes in total; when
// that cap cuts a read short, verified is false because the declared size can
// no longer be checked (the header may still be honest). Reading never
// allocates more than the ScanBytesPerFile buffer plus one small discard
// buffer.
func (s *scanner) readForSize(f *zip.File) (content []byte, counted uint64, verified bool, err error) {
	rc, err := f.Open()
	if err != nil {
		// Unsupported compression or corrupt local data: byte checks
		// against the declared size cannot make sense here.
		return nil, 0, false, err
	}
	defer rc.Close()

	declared := f.UncompressedSize64
	verified = true
	capN := declared + 1 // hard read cap; a small overflow cannot happen for realistic sizes
	if capN == 0 {
		capN = ^uint64(0)
	}
	if budget := uint64(s.fullLeft); capN > budget {
		capN = budget
		verified = false
	}
	n := uint64(len(s.buf))
	if n > capN {
		n = capN
	}

	var read uint64
	if n > 0 {
		n1, _ := io.ReadFull(rc, s.buf[:n])
		read = uint64(n1)
		content = s.buf[:n1]
		if read == n {
			rem := capN - n
			if rem > 1<<40 { // no real archive is this big; guard the int64 cast regardless
				rem = 1 << 40
			}
			copied, _ := io.Copy(io.Discard, io.LimitReader(rc, int64(rem)))
			read += uint64(copied)
		}
	}
	s.decompressed += read
	if left := s.fullLeft - int64(read); left > 0 {
		s.fullLeft = left
	} else {
		s.fullLeft = 0
	}
	return content, read, verified, nil
}

// readPrefix reads the first bytes of one entry for content checks only. It
// is used for entries that are already rejected and for entries whose
// declared size pushed the running total over the limit, which must not get
// a full-size read. The window budget is shared across the whole scan, so
// such entries together never decompress more than ScanBytesPerFile bytes.
func (s *scanner) readPrefix(f *zip.File) (content []byte, err error) {
	rc, err := f.Open()
	if err != nil {
		return nil, err
	}
	defer rc.Close()

	n := int64(len(s.buf))
	if n > s.prefixLeft {
		n = s.prefixLeft
	}
	if n <= 0 {
		return nil, nil
	}
	n1, _ := io.ReadFull(rc, s.buf[:n])
	if n1 <= 0 {
		return nil, nil
	}
	s.decompressed += uint64(n1)
	s.prefixLeft -= int64(n1)
	return s.buf[:n1], nil
}

// shortName truncates a path for use inside a message.
func shortName(name string) string {
	const max = 100
	if len(name) <= max {
		return name
	}
	return name[:max] + "..."
}

func lowerASCII(s string) string {
	b := []byte(s)
	for i, c := range b {
		if 'A' <= c && c <= 'Z' {
			b[i] = c + ('a' - 'A')
		}
	}
	return string(b)
}

// sortFindings orders errors first, then by Path, then by Code. This is the
// order the report keeps, so callers get stable output.
func sortFindings(fs []Finding) {
	sort.SliceStable(fs, func(i, j int) bool {
		if fs[i].Severity != fs[j].Severity {
			return fs[i].Severity == SeverityError
		}
		if fs[i].Path != fs[j].Path {
			return fs[i].Path < fs[j].Path
		}
		return fs[i].Code < fs[j].Code
	})
}
