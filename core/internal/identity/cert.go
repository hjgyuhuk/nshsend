package identity

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"math/big"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// DeviceCertificate holds the device's RSA-2048 key pair, self-signed certificate,
// and uppercase hex SHA-256 DER fingerprint.
type DeviceCertificate struct {
	PrivateKeyPEM  string
	CertificatePEM string
	Fingerprint    string
	TLSCert        tls.Certificate
}

type persistedIdentity struct {
	PrivateKeyPEM  string `json:"privateKeyPem"`
	CertificatePEM string `json:"certificatePem"`
}

// GenerateDeviceCertificate creates a new RSA-2048 self-signed certificate matching LocalSend's identity format.
func GenerateDeviceCertificate() (*DeviceCertificate, error) {
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return nil, fmt.Errorf("generate rsa key: %w", err)
	}

	privDER, err := x509.MarshalPKCS8PrivateKey(priv)
	if err != nil {
		return nil, fmt.Errorf("marshal private key: %w", err)
	}

	serialLimit := new(big.Int).Lsh(big.NewInt(1), 128)
	serial, err := rand.Int(rand.Reader, serialLimit)
	if err != nil {
		return nil, fmt.Errorf("generate serial: %w", err)
	}

	now := time.Now().UTC()
	tmpl := &x509.Certificate{
		SerialNumber: serial,
		Subject: pkix.Name{
			CommonName: "LocalSend User",
		},
		NotBefore:             now.Add(-24 * time.Hour),
		NotAfter:              now.AddDate(100, 0, 0),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth, x509.ExtKeyUsageClientAuth},
		BasicConstraintsValid: true,
	}

	certDER, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &priv.PublicKey, priv)
	if err != nil {
		return nil, fmt.Errorf("create certificate: %w", err)
	}

	privPEM := string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: privDER}))
	certPEM := string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certDER}))

	tlsCert, err := tls.X509KeyPair([]byte(certPEM), []byte(privPEM))
	if err != nil {
		return nil, fmt.Errorf("parse generated keypair: %w", err)
	}

	return &DeviceCertificate{
		PrivateKeyPEM:  privPEM,
		CertificatePEM: certPEM,
		Fingerprint:    ComputeCertFingerprintDER(certDER),
		TLSCert:        tlsCert,
	}, nil
}

// LoadOrCreateDeviceCertificate loads <dataDir>/identity.json or generates and persists a new identity.
func LoadOrCreateDeviceCertificate(dataDir string) (*DeviceCertificate, error) {
	if err := os.MkdirAll(dataDir, 0o700); err != nil {
		return nil, fmt.Errorf("create data dir: %w", err)
	}
	path := filepath.Join(dataDir, "identity.json")
	raw, err := os.ReadFile(path)
	if err == nil {
		var saved persistedIdentity
		if jsonErr := json.Unmarshal(raw, &saved); jsonErr == nil && saved.PrivateKeyPEM != "" && saved.CertificatePEM != "" {
			if parsed, parseErr := parseDeviceCertificate(saved.PrivateKeyPEM, saved.CertificatePEM); parseErr == nil {
				return parsed, nil
			}
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("read identity file: %w", err)
	}

	generated, err := GenerateDeviceCertificate()
	if err != nil {
		return nil, err
	}
	payload, err := json.MarshalIndent(persistedIdentity{
		PrivateKeyPEM:  generated.PrivateKeyPEM,
		CertificatePEM: generated.CertificatePEM,
	}, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("marshal identity: %w", err)
	}
	if err := writeAtomicFile(path, payload, 0o600); err != nil {
		return nil, fmt.Errorf("persist identity: %w", err)
	}
	return generated, nil
}

func parseDeviceCertificate(privPEM, certPEM string) (*DeviceCertificate, error) {
	tlsCert, err := tls.X509KeyPair([]byte(certPEM), []byte(privPEM))
	if err != nil {
		return nil, err
	}
	if len(tlsCert.Certificate) == 0 {
		return nil, errors.New("empty certificate chain")
	}
	return &DeviceCertificate{
		PrivateKeyPEM:  privPEM,
		CertificatePEM: certPEM,
		Fingerprint:    ComputeCertFingerprintDER(tlsCert.Certificate[0]),
		TLSCert:        tlsCert,
	}, nil
}

// ComputeCertFingerprintDER returns the uppercase hex SHA-256 digest of DER certificate bytes.
func ComputeCertFingerprintDER(der []byte) string {
	sum := sha256.Sum256(der)
	return strings.ToUpper(hex.EncodeToString(sum[:]))
}

// VerifyPeerSelfSignedCert parses the peer leaf certificate, validates time validity and self-signature,
// and optionally checks that its SHA-256 fingerprint matches expectedFingerprint (case-insensitive).
func VerifyPeerSelfSignedCert(rawCerts [][]byte, expectedFingerprint string) (string, error) {
	if len(rawCerts) == 0 {
		return "", errors.New("peer presented no certificate")
	}
	leaf, err := x509.ParseCertificate(rawCerts[0])
	if err != nil {
		return "", fmt.Errorf("parse peer certificate: %w", err)
	}
	now := time.Now().UTC()
	if now.Before(leaf.NotBefore) || now.After(leaf.NotAfter) {
		return "", errors.New("peer certificate is expired or not yet valid")
	}
	if err := leaf.CheckSignature(leaf.SignatureAlgorithm, leaf.RawTBSCertificate, leaf.Signature); err != nil {
		return "", fmt.Errorf("peer certificate signature invalid: %w", err)
	}
	fp := ComputeCertFingerprintDER(rawCerts[0])
	if expectedFingerprint != "" && !strings.EqualFold(fp, expectedFingerprint) {
		return "", fmt.Errorf("peer certificate fingerprint mismatch: got %s, want %s", fp, expectedFingerprint)
	}
	return fp, nil
}

func writeAtomicFile(path string, data []byte, mode os.FileMode) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".tmp-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.Remove(tmpName)
		}
	}()
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Chmod(mode); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpName, path); err != nil {
		return err
	}
	cleanup = false
	return nil
}
