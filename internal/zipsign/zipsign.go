// Package zipsign hashes a build zip and signs a statement about it, so the server and the developer can check later
// that the bytes uploaded are the bytes that were scanned. The signature proves who held the key and that the zip
// was not changed. It does not prove the zip is safe.
package zipsign

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/vapour-computer/vapour-cli/internal/config"
	"github.com/vapour-computer/vapour-cli/internal/zipscan"
)

const (
	// Scanner names the rule set that produced the scan result.
	Scanner = "vapour-zipscan/1"
	// domain is prepended to the canonical JSON before signing, so the signature cannot be replayed as another message type.
	domain      = "vapour-zip-attestation-v1\n"
	keyPrefix   = "vapour-ed25519-v1:"
	keyFileName = "signing.key"
	// FileName is the attestation file written next to a zip, appended to the zip's name.
	FileName = ".attestation.json"
)

type ScanResult struct {
	Result   string            `json:"result"` // "pass" or "warn"; a build with errors is never signed
	Findings []zipscan.Finding `json:"findings"`
}

type Attestation struct {
	Version   int        `json:"version"`
	SHA256    string     `json:"sha256"`
	Size      int64      `json:"size"`
	Entries   int        `json:"entries"`
	Scanner   string     `json:"scanner"`
	Scan      ScanResult `json:"scan"`
	SignedAt  string     `json:"signed_at"`
	KeyID     string     `json:"key_id"`
	PublicKey string     `json:"public_key"`
	Signature string     `json:"signature,omitempty"`
}

// Error is a verification failure with a stable code.
type Error struct{ Code, Message string }

func (e *Error) Error() string { return e.Message }

// HashFile returns the lowercase hex SHA-256 and the size of the file at path.
func HashFile(path string) (string, int64, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", 0, err
	}
	defer f.Close()
	h := sha256.New()
	n, err := io.Copy(h, f)
	if err != nil {
		return "", 0, err
	}
	return hex.EncodeToString(h.Sum(nil)), n, nil
}

// KeyID is the first 16 hex characters of the SHA-256 of the public key.
func KeyID(pub ed25519.PublicKey) string {
	sum := sha256.Sum256(pub)
	return hex.EncodeToString(sum[:])[:16]
}

// KeyPath is where the signing key lives.
func KeyPath() (string, error) {
	d, err := config.Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(d, keyFileName), nil
}

// LoadOrCreateKey reads the signing key, creating it (mode 0600) on first use. A key file readable by other users is refused.
func LoadOrCreateKey() (priv ed25519.PrivateKey, created bool, err error) {
	p, err := KeyPath()
	if err != nil {
		return nil, false, err
	}
	b, err := os.ReadFile(p)
	if errors.Is(err, os.ErrNotExist) {
		return createKey(p)
	}
	if err != nil {
		return nil, false, err
	}
	if st, err := os.Stat(p); err == nil && st.Mode().Perm()&0o077 != 0 {
		return nil, false, fmt.Errorf("%s is readable by other users (mode %o). Run: chmod 600 %s", p, st.Mode().Perm(), p)
	}
	s := strings.TrimSpace(string(b))
	if !strings.HasPrefix(s, keyPrefix) {
		return nil, false, fmt.Errorf("%s is not a Vapour signing key. Delete it to create a new one", p)
	}
	seed, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(s, keyPrefix))
	if err != nil || len(seed) != ed25519.SeedSize {
		return nil, false, fmt.Errorf("%s is damaged. Delete it to create a new one", p)
	}
	return ed25519.NewKeyFromSeed(seed), false, nil
}

func createKey(p string) (ed25519.PrivateKey, bool, error) {
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		return nil, false, err
	}
	seed := make([]byte, ed25519.SeedSize)
	if _, err := rand.Read(seed); err != nil {
		return nil, false, err
	}
	f, err := os.OpenFile(p, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return nil, false, err
	}
	_, werr := f.WriteString(keyPrefix + base64.StdEncoding.EncodeToString(seed) + "\n")
	if cerr := f.Close(); werr == nil {
		werr = cerr
	}
	if werr != nil {
		os.Remove(p)
		return nil, false, werr
	}
	return ed25519.NewKeyFromSeed(seed), true, nil
}

// Sign hashes zipPath and signs a statement that includes the scan result. Reports with errors are refused.
func Sign(zipPath string, rep *zipscan.Report, priv ed25519.PrivateKey, now time.Time) (*Attestation, error) {
	if rep.Failed() {
		return nil, errors.New("refusing to sign a build that failed the scan")
	}
	sum, size, err := HashFile(zipPath)
	if err != nil {
		return nil, err
	}
	pub := priv.Public().(ed25519.PublicKey)
	result := "pass"
	for _, f := range rep.Findings {
		if f.Severity == zipscan.SeverityWarn {
			result = "warn"
		}
	}
	findings := rep.Findings
	if findings == nil {
		findings = []zipscan.Finding{}
	}
	a := &Attestation{
		Version: 1, SHA256: sum, Size: size, Entries: rep.Entries, Scanner: Scanner,
		Scan:     ScanResult{Result: result, Findings: findings},
		SignedAt: now.UTC().Format(time.RFC3339), KeyID: KeyID(pub), PublicKey: base64.StdEncoding.EncodeToString(pub),
	}
	msg, err := a.signedBytes()
	if err != nil {
		return nil, err
	}
	a.Signature = base64.StdEncoding.EncodeToString(ed25519.Sign(priv, msg))
	return a, nil
}

// signedBytes is the domain prefix plus canonical JSON (sorted keys, no spaces, no HTML escaping) without the signature.
func (a *Attestation) signedBytes() ([]byte, error) {
	raw, err := json.Marshal(a)
	if err != nil {
		return nil, err
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil, err
	}
	delete(m, "signature")
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(m); err != nil {
		return nil, err
	}
	return append([]byte(domain), bytes.TrimRight(buf.Bytes(), "\n")...), nil
}

// Verify checks the signature, the key id, and that zipPath still has the hash and size the attestation names.
func Verify(zipPath string, a *Attestation) error {
	if err := VerifySignature(a); err != nil {
		return err
	}
	sum, size, err := HashFile(zipPath)
	if err != nil {
		return err
	}
	if size != a.Size {
		return &Error{"size_mismatch", fmt.Sprintf("the zip is %d bytes but the attestation says %d", size, a.Size)}
	}
	if sum != a.SHA256 {
		return &Error{"hash_mismatch", fmt.Sprintf("the zip's SHA-256 is %s but the attestation says %s", sum, a.SHA256)}
	}
	return nil
}

// VerifySignature checks only the attestation itself: version, key id and signature.
func VerifySignature(a *Attestation) error {
	if a.Version != 1 {
		return &Error{"unsupported_version", fmt.Sprintf("attestation version %d is not supported by this CLI", a.Version)}
	}
	pub, err := base64.StdEncoding.DecodeString(a.PublicKey)
	if err != nil || len(pub) != ed25519.PublicKeySize {
		return &Error{"bad_key", "the attestation has no valid public key"}
	}
	if KeyID(pub) != a.KeyID {
		return &Error{"bad_key_id", "key_id does not match the public key"}
	}
	sig, err := base64.StdEncoding.DecodeString(a.Signature)
	if err != nil || len(sig) != ed25519.SignatureSize {
		return &Error{"bad_signature", "the attestation signature is missing or malformed"}
	}
	msg, err := a.signedBytes()
	if err != nil {
		return err
	}
	if !ed25519.Verify(pub, msg, sig) {
		return &Error{"bad_signature", "the signature does not match the attestation: it was changed after signing"}
	}
	return nil
}

func (a *Attestation) JSON() ([]byte, error) { return json.MarshalIndent(a, "", "  ") }

func Parse(b []byte) (*Attestation, error) {
	var a Attestation
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&a); err != nil {
		return nil, fmt.Errorf("not a Vapour attestation: %w", err)
	}
	return &a, nil
}
