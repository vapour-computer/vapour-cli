package zipscan

import (
	"bytes"
	"fmt"
	"regexp"
	"strings"
)

var (
	wasmPrefix = []byte{0x00, 'a', 's', 'm'}
	shebang    = []byte{'#', '!'}
)

// nestedArchiveExts: entries with these suffixes look like archives inside the
// bundle. Note there is deliberately no ".gz"/".br": those are normal
// precompressed web assets.
var nestedArchiveExts = []string{"zip", "7z", "rar", "tar", "tgz"}

var nativeExecutableExts = []string{
	"exe", "dll", "so", "dylib", "msi", "scr", "bat", "cmd", "ps1", "vbs", "jar", "apk",
}

// harmlessExts are content a user would recognize; executableExts can run
// code. Double extensions pair the two, like "game.png.exe" or
// "readme.txt.js.exe".
var harmlessExts = []string{
	"png", "jpg", "jpeg", "gif", "webp", "svg", "bmp", "ico",
	"mp3", "wav", "ogg", "ogv", "mp4", "webm", "weba", "m4a",
	"txt", "pdf", "json", "csv", "xml", "md", "wasm",
}

var executableExts = []string{
	"exe", "dll", "so", "dylib", "msi", "scr", "bat", "cmd", "com", "pif",
	"ps1", "vbs", "vbe", "js", "mjs", "jse", "wsf", "wsh", "hta", "apk",
}

// checkName runs the checks that only need the entry name. Directory entries
// keep their trailing "/" but are treated as their bare name.
func (s *scanner) checkName(name string) {
	base := strings.TrimSuffix(name, "/")
	ext := extOnly(base)

	if hasSuffixExt(base, nestedArchiveExts) {
		s.add(Finding{
			Severity: SeverityWarn,
			Code:     "nested_archive",
			Path:     name,
			Message:  "Entry looks like a nested archive; publish bundles should contain flat web files.",
		})
	}
	if hasSuffixExt(ext, nativeExecutableExts) {
		s.add(Finding{
			Severity: SeverityError,
			Code:     "native_extension",
			Path:     name,
			Message:  fmt.Sprintf("File has a %q extension, which does not belong in a web bundle.", "."+ext),
		})
	}
	if parts := strings.Split(base, "."); len(parts) >= 3 {
		tail, prev := parts[len(parts)-1], parts[len(parts)-2]
		if hasSuffixExt(prev, harmlessExts) && hasSuffixExt(tail, executableExts) {
			s.add(Finding{
				Severity: SeverityWarn,
				Code:     "double_extension",
				Path:     name,
				Message: fmt.Sprintf("File name ends in %q after a harmless %q, the shape of an executable hiding as a document.",
					"."+tail, "."+prev),
			})
		}
	}
}

// checkContent runs the content checks on the scanned prefix of one file.
func (s *scanner) checkContent(name string, content []byte) {
	if len(content) == 0 {
		return
	}
	// .wasm is a normal prebuilt web asset and must never raise this.
	if !bytes.HasPrefix(content, wasmPrefix) && (isELF(content) || isPE(content) || isMachO(content)) {
		s.add(Finding{
			Severity: SeverityError,
			Code:     "native_executable",
			Path:     name,
			Message:  "File starts with native executable bytes (ELF, PE or Mach-O), whatever its extension says.",
		})
	}
	if bytes.HasPrefix(content, shebang) {
		s.add(Finding{
			Severity: SeverityWarn,
			Code:     "script_shebang",
			Path:     name,
			Message:  "File starts with a #! shebang, the shape of a script a web bundle does not carry.",
		})
	}
	if bytes.Contains(content, eicarString()) {
		s.add(Finding{
			Severity: SeverityError,
			Code:     "test_malware_signature",
			Path:     name,
			Message:  "File contains the EICAR antivirus test signature.",
		})
	}
	s.checkText(name, content)
}

// checkText runs checks that only make sense on textual content. Text is
// recognized by the absence of a NUL byte, which keeps engine binaries and
// compressed assets out of string matching.
func (s *scanner) checkText(name string, content []byte) {
	ext := extOnly(strings.TrimSuffix(name, "/"))
	isScriptLike := ext == "js" || ext == "mjs" || ext == "html" || ext == "htm"
	if !isScriptLike && bytes.IndexByte(content, 0) >= 0 {
		return
	}
	lowered := lowerASCIIBytes(content)

	for _, n := range cryptoMinerNeedles {
		if bytes.Contains(lowered, n) {
			s.add(Finding{
				Severity: SeverityError,
				Code:     "crypto_miner",
				Path:     name,
				Message:  "File contains text commonly found in browser crypto-miners.",
			})
			break
		}
	}
	if !isScriptLike {
		return
	}
	if reason := obfuscationReason(lowered); reason != "" {
		s.add(Finding{
			Severity: SeverityWarn,
			Code:     "obfuscated_script",
			Path:     name,
			Message: fmt.Sprintf("Script uses a typical obfuscation pattern (%s), which does not look like plain game code.",
				reason),
		})
	}
	if ext == "html" || ext == "htm" {
		if match := insecureScriptRe.FindSubmatchIndex(content); match != nil {
			s.add(Finding{
				Severity: SeverityWarn,
				Code:     "external_script_insecure",
				Path:     name,
				Message: fmt.Sprintf("HTML file loads a script from %s, which is not its own bundled https asset.",
					quoteSnippet(content, match[0], match[1])),
			})
		}
	}
}

func isELF(b []byte) bool {
	return len(b) >= 4 && bytes.Equal(b[:4], []byte{0x7f, 'E', 'L', 'F'})
}

// isPE reports the "MZ" DOS-header case: at 0x3c a little-endian word holds
// the offset of the "PE\0\0" signature, which must sit inside the read bytes.
func isPE(b []byte) bool {
	if len(b) < 0x40 || b[0] != 'M' || b[1] != 'Z' {
		return false
	}
	off := int(uint32(b[0x3c]) | uint32(b[0x3d])<<8 | uint32(b[0x3e])<<16 | uint32(b[0x3f])<<24)
	return off >= 0 && off+4 <= len(b) && bytes.Equal(b[off:off+4], []byte{'P', 'E', 0, 0})
}

// machOmagics lists both byte orders of every Mach-O magic.
var machOmagics = [][]byte{
	{0xfe, 0xed, 0xfa, 0xce}, // 0xFEEDFACE
	{0xce, 0xfa, 0xed, 0xfe}, // reversed
	{0xfe, 0xed, 0xfa, 0xcf}, // 0xFEEDFACF
	{0xcf, 0xfa, 0xed, 0xfe}, // reversed
	{0xca, 0xfe, 0xba, 0xbe}, // 0xCAFEBABE
	{0xbe, 0xba, 0xfe, 0xca}, // reversed
	{0xec, 0xdf, 0xce, 0xfa}, // 0xECDFACEF, byte-reversed 0xFEEDFACE
	{0xfa, 0xce, 0xdf, 0xec}, // reversed
}

func isMachO(b []byte) bool {
	if len(b) < 4 {
		return false
	}
	for _, m := range machOmagics {
		if bytes.Equal(b[:4], m) {
			return true
		}
	}
	return false
}

// The EICAR string is built from split parts so this source (and any binary
// compiled from it) never contains the literal antimalware test signature.
var (
	eicarPart1 = "X5O!P%@AP[4\\PZX54(P^)7CC)7}$"
	eicarPart2 = "EICAR-STANDARD-ANTIVIRUS-TEST-FILE!"
	eicarPart3 = "$H+H*"
)

func eicarString() []byte {
	return []byte(eicarPart1 + eicarPart2 + eicarPart3)
}

var cryptoMinerNeedles = [][]byte{
	[]byte("coinhive"),
	[]byte("cryptonight"),
	[]byte("stratum+tcp://"),
	[]byte("coin-hive"),
	[]byte("webminerpool"),
	[]byte("minero.cc"),
}

var obfuscatedNeedles = [][]byte{
	[]byte("eval(atob("),
	[]byte("eval(unescape("),
	[]byte("document.write(unescape("),
}

// fromCharCodeCount is how many "String.fromCharCode(" calls mark obfuscated
// code; normal engine bundles use none or only a handful.
const fromCharCodeCount = 20

// longLineEvalChars is the line length above which a line containing "eval("
// counts as smuggled logic rather than normal script.
const longLineEvalChars = 200000

// obfuscationReason returns the matched pattern, or "" for clean content.
func obfuscationReason(lowered []byte) string {
	for _, n := range obfuscatedNeedles {
		if bytes.Contains(lowered, n) {
			return string(n)
		}
	}
	if bytes.Count(lowered, []byte("string.fromcharcode(")) >= fromCharCodeCount {
		return "String.fromCharCode( repeated 20+ times"
	}
	// One long line calling eval( — but only then: minified engine code
	// without it is normal and stays clean.
	start := 0
	for start < len(lowered) {
		end := bytes.IndexByte(lowered[start:], '\n')
		if end < 0 {
			end = len(lowered)
		} else {
			end += start
		}
		if end-start > longLineEvalChars && bytes.Contains(lowered[start:end], []byte("eval(")) {
			return fmt.Sprintf("a %d-character line calling eval(", end-start)
		}
		start = end + 1
	}
	return ""
}

// insecureScriptRe matches a script tag asking for a plain-http URL or a
// protocol-relative URL to a bare IP address.
var insecureScriptRe = regexp.MustCompile(
	`(?i)<script[^>]*\bsrc\s*=\s*"(?:http://|//[0-9]{1,3}\.[0-9]{1,3}\.[0-9]{1,3}\.[0-9]{1,3})`)

// quoteSnippet returns the matched text as a quoted string, cut short when
// the tag is long.
func quoteSnippet(content []byte, lo, hi int) string {
	const max = 100
	if hi-lo > max {
		hi = lo + max
	}
	return fmt.Sprintf("%q", string(content[lo:hi]))
}

// extOnly returns the lowercase extension of name without the leading dot,
// or "" when there is none.
func extOnly(name string) string {
	i := strings.LastIndexByte(name, '.')
	if i <= 0 || i == len(name)-1 {
		return ""
	}
	return lowerASCII(name[i+1:])
}

// hasSuffixExt reports whether s is (ends with) one of the listed extensions.
func hasSuffixExt(s string, exts []string) bool {
	s = lowerASCII(s)
	for _, e := range exts {
		if s == e || strings.HasSuffix(s, "."+e) {
			return true
		}
	}
	return false
}

// lowerASCIIBytes returns a lowercased copy for ASCII letters only; the
// needle sets and extensions above are ASCII, so this is enough for all
// case-insensitive matching here.
func lowerASCIIBytes(src []byte) []byte {
	dst := make([]byte, len(src))
	for i, c := range src {
		if 'A' <= c && c <= 'Z' {
			c += 'a' - 'A'
		}
		dst[i] = c
	}
	return dst
}
