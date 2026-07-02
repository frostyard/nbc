package pkg

import (
	"context"
	"crypto/ecdsa"
	"crypto/sha256"
	"crypto/x509"
	_ "embed"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"os"

	"github.com/frostyard/std/reporter"
	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/google/go-containerregistry/pkg/name"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/remote"
)

const (
	cosignSignatureAnnotation   = "dev.cosignproject.cosign/signature"
	cosignCertificateAnnotation = "dev.sigstore.cosign/certificate"
	// maxSignaturePayloadBytes caps the simple-signing payload read. Real
	// payloads are a few hundred bytes; this guards against a hostile blob.
	maxSignaturePayloadBytes = 1 << 20
)

// embeddedCosignPub is the frostyard cosign public key baked into the binary.
// It is the default trust anchor used to verify image signatures before
// extraction. Callers can override it with a different key via configuration.
//
//go:embed keys/cosign.pub
var embeddedCosignPub []byte

// cosignSimpleSigningPayload is the minimal shape of a cosign "simple signing"
// payload that we care about: the image digest the signature is bound to.
type cosignSimpleSigningPayload struct {
	Critical struct {
		Image struct {
			DockerManifestDigest string `json:"docker-manifest-digest"`
		} `json:"image"`
		Type string `json:"type"`
	} `json:"critical"`
}

// embeddedCosignKey parses the built-in cosign public key.
func embeddedCosignKey() (*ecdsa.PublicKey, error) {
	return parseCosignPublicKey(embeddedCosignPub)
}

// parseCosignPublicKey parses a PEM-encoded PKIX ECDSA public key (the format
// cosign generate-key-pair produces).
func parseCosignPublicKey(pemBytes []byte) (*ecdsa.PublicKey, error) {
	block, _ := pem.Decode(pemBytes)
	if block == nil {
		return nil, fmt.Errorf("cosign public key is not valid PEM")
	}
	pub, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("failed to parse cosign public key: %w", err)
	}
	ecPub, ok := pub.(*ecdsa.PublicKey)
	if !ok {
		return nil, fmt.Errorf("cosign public key is not an ECDSA key (got %T)", pub)
	}
	return ecPub, nil
}

// verifyCosignPayload verifies a cosign key-based signature over a simple-signing
// payload and binds it to the expected image digest.
//
//   - sigB64 is the base64-encoded ASN.1 ECDSA signature (the value of the
//     dev.cosignproject.cosign/signature annotation on the .sig layer).
//   - payload is the raw simple-signing JSON blob that was signed.
//   - wantDigest is the digest (e.g. "sha256:...") of the image being installed.
//
// It fails if the signature does not verify under pub, or if the signed
// docker-manifest-digest does not match wantDigest (which prevents replaying a
// valid signature onto a different image).
func verifyCosignPayload(pub *ecdsa.PublicKey, payload []byte, sigB64, wantDigest string) error {
	sig, err := base64.StdEncoding.DecodeString(sigB64)
	if err != nil {
		return fmt.Errorf("failed to decode signature: %w", err)
	}

	digest := sha256.Sum256(payload)
	if !ecdsa.VerifyASN1(pub, digest[:], sig) {
		return fmt.Errorf("signature does not verify against the trusted public key")
	}

	var p cosignSimpleSigningPayload
	if err := json.Unmarshal(payload, &p); err != nil {
		return fmt.Errorf("failed to parse signing payload: %w", err)
	}
	if p.Critical.Image.DockerManifestDigest == "" {
		return fmt.Errorf("signing payload has no image digest")
	}
	if p.Critical.Image.DockerManifestDigest != wantDigest {
		return fmt.Errorf("signature is for a different image: signed %s, want %s",
			p.Critical.Image.DockerManifestDigest, wantDigest)
	}
	return nil
}

// verifyPulledImage verifies a registry-pulled image's cosign key-based
// signature before it is trusted (extracted or cached), unless skipVerify is
// set. It is shared by the container extractor and the cache downloader so both
// registry-pull paths enforce the same policy.
func verifyPulledImage(ctx context.Context, ref name.Reference, img v1.Image, skipVerify bool, cosignKeyPath string, progress reporter.Reporter) error {
	if skipVerify {
		if progress != nil {
			progress.Warning("Skipping image signature verification (--insecure-skip-verify)")
		}
		return nil
	}

	pub, err := resolveCosignKey(cosignKeyPath)
	if err != nil {
		return fmt.Errorf("failed to load cosign public key: %w", err)
	}
	digest, err := img.Digest()
	if err != nil {
		return fmt.Errorf("failed to resolve image digest: %w", err)
	}

	if progress != nil {
		progress.Message("Verifying image signature...")
	}
	if err := verifyImageSignature(ctx, ref, digest, pub); err != nil {
		return fmt.Errorf("image signature verification failed: %w\n\n"+
			"The image is not signed by the trusted key. Refusing to use it.\n"+
			"Use --cosign-key to trust a different key, or --insecure-skip-verify to bypass verification (not recommended)", err)
	}
	if progress != nil {
		progress.Message("Image signature verified")
	}
	return nil
}

// resolveCosignKey returns the public key from keyPath, or the embedded key when
// keyPath is empty.
func resolveCosignKey(keyPath string) (*ecdsa.PublicKey, error) {
	if keyPath != "" {
		data, err := os.ReadFile(keyPath)
		if err != nil {
			return nil, fmt.Errorf("failed to read cosign key %s: %w", keyPath, err)
		}
		return parseCosignPublicKey(data)
	}
	return embeddedCosignKey()
}

// verifyImageSignature verifies that the image at ref (with the given digest)
// carries a valid cosign key-based signature under pub. It fetches the cosign
// signature artifact (the "<algo>-<hex>.sig" tag), finds a key-based signature
// layer (ignoring keyless certificate layers and any co-published attestations),
// and verifies it, binding the signature to the image digest.
func verifyImageSignature(ctx context.Context, ref name.Reference, digest v1.Hash, pub *ecdsa.PublicKey) error {
	opts := []remote.Option{
		remote.WithAuthFromKeychain(authn.DefaultKeychain),
		remote.WithContext(ctx),
	}

	sigTag := ref.Context().Tag(fmt.Sprintf("%s-%s.sig", digest.Algorithm, digest.Hex))
	sigImg, err := remote.Image(sigTag, opts...)
	if err != nil {
		return fmt.Errorf("no cosign signature found at %s: %w", sigTag.String(), err)
	}
	manifest, err := sigImg.Manifest()
	if err != nil {
		return fmt.Errorf("failed to read signature manifest: %w", err)
	}

	var lastErr error
	foundKeyLayer := false
	for _, layer := range manifest.Layers {
		sigB64, ok := layer.Annotations[cosignSignatureAnnotation]
		if !ok {
			continue
		}
		// Only key-based signatures are handled here; skip keyless (certificate)
		// layers so a co-published provenance attestation cannot interfere.
		if _, hasCert := layer.Annotations[cosignCertificateAnnotation]; hasCert {
			continue
		}
		foundKeyLayer = true

		payload, err := fetchSignatureBlob(ref.Context(), layer.Digest, opts)
		if err != nil {
			lastErr = err
			continue
		}
		if err := verifyCosignPayload(pub, payload, sigB64, digest.String()); err != nil {
			lastErr = err
			continue
		}
		return nil // a signature verified
	}

	if !foundKeyLayer {
		return fmt.Errorf("image %s has no key-based cosign signature", ref.String())
	}
	return fmt.Errorf("no valid signature for image %s: %w", ref.String(), lastErr)
}

// fetchSignatureBlob downloads the simple-signing payload blob and verifies its
// content matches the expected digest.
func fetchSignatureBlob(repo name.Repository, digest v1.Hash, opts []remote.Option) ([]byte, error) {
	layer, err := remote.Layer(repo.Digest(digest.String()), opts...)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch signature payload: %w", err)
	}
	rc, err := layer.Compressed()
	if err != nil {
		return nil, fmt.Errorf("failed to read signature payload: %w", err)
	}
	defer func() { _ = rc.Close() }()

	data, err := io.ReadAll(io.LimitReader(rc, maxSignaturePayloadBytes))
	if err != nil {
		return nil, fmt.Errorf("failed to read signature payload: %w", err)
	}
	if got := fmt.Sprintf("sha256:%x", sha256.Sum256(data)); got != digest.String() {
		return nil, fmt.Errorf("signature payload digest mismatch: got %s, want %s", got, digest.String())
	}
	return data, nil
}
