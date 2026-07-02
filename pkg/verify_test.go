package pkg

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/pem"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const realImageDigest = "sha256:3a50babd974dbf401aa3a5eece41d98d856ebc4b248917427b6645b2f4606ead"

func readFixture(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", "cosign", name))
	if err != nil {
		t.Fatalf("read fixture %s: %v", name, err)
	}
	return b
}

// TestVerifyCosignPayload_RealSignature verifies the actual production signature
// of ghcr.io/frostyard/snowloaded against the embedded public key. This is a
// real cosign key-based signature captured from the registry.
func TestVerifyCosignPayload_RealSignature(t *testing.T) {
	pub, err := parseCosignPublicKey(readFixture(t, "../../keys/cosign.pub"))
	if err != nil {
		t.Fatalf("parse embedded key: %v", err)
	}
	payload := readFixture(t, "snowloaded.payload.json")
	sigB64 := strings.TrimSpace(string(readFixture(t, "snowloaded.sig.b64")))

	if err := verifyCosignPayload(pub, payload, sigB64, realImageDigest); err != nil {
		t.Errorf("real signature should verify: %v", err)
	}
}

// TestVerifyCosignPayload_WrongDigestRejected ensures the payload's signed digest
// is bound to the image being installed, so a valid signature can't be replayed
// onto a different image.
func TestVerifyCosignPayload_WrongDigestRejected(t *testing.T) {
	pub, err := parseCosignPublicKey(readFixture(t, "../../keys/cosign.pub"))
	if err != nil {
		t.Fatal(err)
	}
	payload := readFixture(t, "snowloaded.payload.json")
	sigB64 := strings.TrimSpace(string(readFixture(t, "snowloaded.sig.b64")))

	other := "sha256:0000000000000000000000000000000000000000000000000000000000000000"
	if err := verifyCosignPayload(pub, payload, sigB64, other); err == nil {
		t.Error("signature for a different digest must be rejected (digest binding)")
	}
}

func TestVerifyCosignPayload_TamperedPayloadRejected(t *testing.T) {
	pub, err := parseCosignPublicKey(readFixture(t, "../../keys/cosign.pub"))
	if err != nil {
		t.Fatal(err)
	}
	payload := readFixture(t, "snowloaded.payload.json")
	sigB64 := strings.TrimSpace(string(readFixture(t, "snowloaded.sig.b64")))

	tampered := append([]byte{}, payload...)
	tampered[10] ^= 0xff
	if err := verifyCosignPayload(pub, tampered, sigB64, realImageDigest); err == nil {
		t.Error("a tampered payload must fail signature verification")
	}
}

func TestVerifyCosignPayload_WrongKeyRejected(t *testing.T) {
	// A different key must not verify the real signature.
	otherKey, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	payload := readFixture(t, "snowloaded.payload.json")
	sigB64 := strings.TrimSpace(string(readFixture(t, "snowloaded.sig.b64")))

	if err := verifyCosignPayload(&otherKey.PublicKey, payload, sigB64, realImageDigest); err == nil {
		t.Error("real signature must not verify under a different key")
	}
}

// TestVerifyCosignPayload_RoundTrip proves the crypto path with a freshly
// generated key, independent of the fixture.
func TestVerifyCosignPayload_RoundTrip(t *testing.T) {
	key, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	digest := "sha256:" + strings.Repeat("ab", 32)
	payload := []byte(`{"critical":{"image":{"docker-manifest-digest":"` + digest + `"},"type":"cosign container image signature"}}`)
	h := sha256.Sum256(payload)
	der, err := ecdsa.SignASN1(rand.Reader, key, h[:])
	if err != nil {
		t.Fatal(err)
	}
	sigB64 := base64.StdEncoding.EncodeToString(der)

	if err := verifyCosignPayload(&key.PublicKey, payload, sigB64, digest); err != nil {
		t.Errorf("round-trip verify failed: %v", err)
	}
}

// TestParseCosignPublicKey checks the embedded key parses and that garbage is
// rejected.
func TestParseCosignPublicKey(t *testing.T) {
	pub, err := parseCosignPublicKey(readFixture(t, "../../keys/cosign.pub"))
	if err != nil {
		t.Fatalf("embedded key must parse: %v", err)
	}
	if pub.Curve != elliptic.P256() {
		t.Errorf("expected P-256 key, got %v", pub.Curve)
	}

	if _, err := parseCosignPublicKey([]byte("not a key")); err == nil {
		t.Error("garbage input must fail to parse")
	}

	// A valid PEM that is not an ECDSA key must be rejected.
	block := &pem.Block{Type: "PUBLIC KEY", Bytes: []byte("bogus")}
	if _, err := parseCosignPublicKey(pem.EncodeToMemory(block)); err == nil {
		t.Error("non-ECDSA PEM must fail to parse")
	}
}

// TestEmbeddedCosignKeyLoads ensures the key baked into the binary is usable.
func TestEmbeddedCosignKeyLoads(t *testing.T) {
	pub, err := embeddedCosignKey()
	if err != nil {
		t.Fatalf("embedded cosign key failed to load: %v", err)
	}
	if pub == nil {
		t.Fatal("embedded cosign key is nil")
	}
}
