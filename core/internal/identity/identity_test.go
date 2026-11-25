package identity

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"math/big"
	"strings"
	"testing"
	"time"
)

func TestDeviceCertificateRoundTripAndVerification(t *testing.T) {
	dir := t.TempDir()
	first, err := LoadOrCreateDeviceCertificate(dir)
	if err != nil {
		t.Fatalf("LoadOrCreateDeviceCertificate first: %v", err)
	}
	if len(first.Fingerprint) != 64 || strings.ToUpper(first.Fingerprint) != first.Fingerprint {
		t.Fatalf("unexpected fingerprint format: %q", first.Fingerprint)
	}

	second, err := LoadOrCreateDeviceCertificate(dir)
	if err != nil {
		t.Fatalf("LoadOrCreateDeviceCertificate second: %v", err)
	}
	if first.Fingerprint != second.Fingerprint {
		t.Fatalf("identity was regenerated: %s != %s", first.Fingerprint, second.Fingerprint)
	}

	fp, err := VerifyPeerSelfSignedCert(first.TLSCert.Certificate, strings.ToLower(first.Fingerprint))
	if err != nil {
		t.Fatalf("VerifyPeerSelfSignedCert valid failed: %v", err)
	}
	if fp != first.Fingerprint {
		t.Fatalf("fingerprint mismatch: %s != %s", fp, first.Fingerprint)
	}

	if _, err := VerifyPeerSelfSignedCert(first.TLSCert.Certificate, strings.Repeat("0", 64)); err == nil {
		t.Fatalf("expected fingerprint mismatch error")
	}

	// Expired cert must fail verification.
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	expiredTmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "Expired"},
		NotBefore:    time.Now().Add(-48 * time.Hour),
		NotAfter:     time.Now().Add(-24 * time.Hour),
	}
	expiredDER, err := x509.CreateCertificate(rand.Reader, expiredTmpl, expiredTmpl, &priv.PublicKey, priv)
	if err != nil {
		t.Fatalf("create expired cert: %v", err)
	}
	if _, err := VerifyPeerSelfSignedCert([][]byte{expiredDER}, ""); err == nil {
		t.Fatalf("expected expired cert error")
	}
}

func TestSettingsStorePersistence(t *testing.T) {
	dir := t.TempDir()
	store, err := LoadSettingsStore(dir)
	if err != nil {
		t.Fatalf("LoadSettingsStore: %v", err)
	}

	newAlias := "Desk A"
	newPort := uint16(54321)
	quick := QuickSaveOn
	updated, needsRestart, err := store.UpdateSettings(SettingsPatch{
		Alias:     &newAlias,
		Port:      &newPort,
		QuickSave: &quick,
	})
	if err != nil {
		t.Fatalf("UpdateSettings: %v", err)
	}
	if !needsRestart {
		t.Fatalf("expected needsRestart=true when alias/port changed")
	}
	if updated.Alias != newAlias || updated.Port != newPort || updated.QuickSave != QuickSaveOn {
		t.Fatalf("unexpected updated settings: %+v", updated)
	}

	reloaded, err := LoadSettingsStore(dir)
	if err != nil {
		t.Fatalf("reload SettingsStore: %v", err)
	}
	if got := reloaded.CurrentSettings(); got.Alias != newAlias || got.Port != newPort {
		t.Fatalf("reloaded settings mismatch: %+v", got)
	}
}
