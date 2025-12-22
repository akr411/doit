package sync

import (
	"crypto/tls"
	"crypto/x509"
	"database/sql"
	"encoding/pem"
	"testing"

	_ "modernc.org/sqlite"
)

func setupTLSTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}

	schema := `
	CREATE TABLE config (key TEXT PRIMARY KEY, value TEXT NOT NULL);
	CREATE TABLE peer_certificates (
		device_id TEXT PRIMARY KEY,
		fingerprint TEXT NOT NULL
	);
	`

	if _, err := db.Exec(schema); err != nil {
		t.Fatal(err)
	}

	return db
}

func TestGenerateSelfSignedCert(t *testing.T) {
	db := setupTLSTestDB(t)
	defer func() { _ = db.Close() }()

	cm := NewCertificateManager(db)

	err := cm.GenerateSelfSignedCert("device-test-123")
	if err != nil {
		t.Fatal(err)
	}

	fingerprint, err := cm.GetFingerprint()
	if err != nil {
		t.Fatal(err)
	}

	if len(fingerprint) != 64 {
		t.Errorf("Expected 64-char hex fingerprint, got %d", len(fingerprint))
	}

	var certPEM, keyPEM string
	err = db.QueryRow(`
		SELECT
			(SELECT value FROM config WHERE key='tls_cert'),
			(SELECT value FROM config WHERE key='tls_key')
	`).Scan(&certPEM, &keyPEM)

	if err != nil {
		t.Fatal("Certificate not stored in database")
	}

	if certPEM == "" || keyPEM == "" {
		t.Error("Certificate or key is empty")
	}
}

func TestCertificateIdempotency(t *testing.T) {
	db := setupTLSTestDB(t)
	defer func() { _ = db.Close() }()

	cm := NewCertificateManager(db)

	err := cm.GenerateSelfSignedCert("device-test")
	if err != nil {
		t.Fatal(err)
	}

	fp1, _ := cm.GetFingerprint()

	err = cm.GenerateSelfSignedCert("device-test")
	if err != nil {
		t.Fatal(err)
	}

	fp2, _ := cm.GetFingerprint()

	if fp1 != fp2 {
		t.Error("Certificate regenerated when it should be reused")
	}
}

func TestGetTLSConfig(t *testing.T) {
	tests := []struct {
		name     string
		isServer bool
		wantAuth tls.ClientAuthType
	}{
		{"server config requires client cert", true, tls.RequireAnyClientCert},
		{"client config no client cert required", false, tls.NoClientCert},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db := setupTLSTestDB(t)
			defer func() { _ = db.Close() }()

			cm := NewCertificateManager(db)
			_ = cm.GenerateSelfSignedCert("device-test")

			config, err := cm.GetTLSConfig(tt.isServer)
			if err != nil {
				t.Fatal(err)
			}

			if config.MinVersion != tls.VersionTLS13 {
				t.Error("Expected TLS 1.3 minimum version")
			}

			if len(config.CipherSuites) != 2 {
				t.Errorf("Expected 2 cipher suites, got %d", len(config.CipherSuites))
			}

			if tt.isServer && config.ClientAuth != tt.wantAuth {
				t.Errorf("Expected ClientAuth %v, got %v", tt.wantAuth, config.ClientAuth)
			}

			if config.VerifyPeerCertificate == nil {
				t.Error("Expected VerifyPeerCertificate callback to be set")
			}
		})
	}
}

func TestSavePeerCertificate(t *testing.T) {
	db := setupTLSTestDB(t)
	defer func() { _ = db.Close() }()

	cm := NewCertificateManager(db)

	peerID := "peer-device-123"
	fingerprint := "abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890"

	err := cm.SavePeerCertificate(peerID, fingerprint)
	if err != nil {
		t.Fatal(err)
	}

	var storedFP string
	err = db.QueryRow(`
		SELECT fingerprint FROM peer_certificates WHERE device_id = ?
	`, peerID).Scan(&storedFP)

	if err != nil {
		t.Fatal("Peer certificate not saved")
	}

	if storedFP != fingerprint {
		t.Errorf("Expected fingerprint %s, got %s", fingerprint, storedFP)
	}
}

func TestSavePeerCertificateUpdate(t *testing.T) {
	db := setupTLSTestDB(t)
	defer func() { _ = db.Close() }()

	cm := NewCertificateManager(db)

	peerID := "peer-device-123"
	fp1 := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	fp2 := "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"

	_ = cm.SavePeerCertificate(peerID, fp1)
	_ = cm.SavePeerCertificate(peerID, fp2)

	var storedFP string
	_ = db.QueryRow(`
		SELECT fingerprint FROM peer_certificates WHERE device_id = ?
	`, peerID).Scan(&storedFP)

	if storedFP != fp2 {
		t.Errorf("Expected fingerprint to be updated to %s, got %s", fp2, storedFP)
	}
}

func TestGetFingerprintNotFound(t *testing.T) {
	db := setupTLSTestDB(t)
	defer func() { _ = db.Close() }()

	cm := NewCertificateManager(db)

	_, err := cm.GetFingerprint()
	if err == nil {
		t.Error("expected error when fingerprint not found")
	}
}

func TestGetTLSConfigNoCert(t *testing.T) {
	db := setupTLSTestDB(t)
	defer func() { _ = db.Close() }()

	cm := NewCertificateManager(db)

	_, err := cm.GetTLSConfig(true)
	if err == nil {
		t.Error("expected error when no cert exists")
	}
}

func TestCheckCertValidity(t *testing.T) {
	db := setupTLSTestDB(t)
	defer func() { _ = db.Close() }()

	cm := NewCertificateManager(db)

	err := cm.CheckCertValidity()
	if err == nil {
		t.Error("expected error when no cert exists")
	}

	_ = cm.GenerateSelfSignedCert("test-device")

	err = cm.CheckCertValidity()
	if err != nil {
		t.Errorf("CheckCertValidity failed for valid cert: %v", err)
	}
}

func TestCheckCertValidityInvalidPEM(t *testing.T) {
	db := setupTLSTestDB(t)
	defer func() { _ = db.Close() }()

	cm := NewCertificateManager(db)

	_, _ = db.Exec(`INSERT INTO config (key, value) VALUES ('tls_cert', 'invalid pem data')`)

	err := cm.CheckCertValidity()
	if err == nil {
		t.Error("expected error for invalid PEM")
	}
}

func TestRegenerateCertIfExpiredNoCert(t *testing.T) {
	db := setupTLSTestDB(t)
	defer func() { _ = db.Close() }()

	cm := NewCertificateManager(db)

	regenerated, err := cm.RegenerateCertIfExpired("test-device")
	if err != nil {
		t.Fatalf("RegenerateCertIfExpired failed: %v", err)
	}

	if !regenerated {
		t.Error("expected cert to be generated when none exists")
	}

	fp, err := cm.GetFingerprint()
	if err != nil {
		t.Error("fingerprint should exist after regeneration")
	}
	if len(fp) != 64 {
		t.Errorf("expected 64-char fingerprint, got %d", len(fp))
	}
}

func TestRegenerateCertIfExpiredValidCert(t *testing.T) {
	db := setupTLSTestDB(t)
	defer func() { _ = db.Close() }()

	cm := NewCertificateManager(db)

	_ = cm.GenerateSelfSignedCert("test-device")
	fp1, _ := cm.GetFingerprint()

	regenerated, err := cm.RegenerateCertIfExpired("test-device")
	if err != nil {
		t.Fatalf("RegenerateCertIfExpired failed: %v", err)
	}

	if regenerated {
		t.Error("valid cert should not be regenerated")
	}

	fp2, _ := cm.GetFingerprint()
	if fp1 != fp2 {
		t.Error("fingerprint should not change for valid cert")
	}
}

func TestRegenerateCertIfExpiredInvalidPEM(t *testing.T) {
	db := setupTLSTestDB(t)
	defer func() { _ = db.Close() }()

	cm := NewCertificateManager(db)

	_, _ = db.Exec(`INSERT INTO config (key, value) VALUES ('tls_cert', 'invalid pem')`)

	regenerated, err := cm.RegenerateCertIfExpired("test-device")
	if err != nil {
		t.Fatalf("RegenerateCertIfExpired failed: %v", err)
	}

	if !regenerated {
		t.Error("expected cert to be regenerated for invalid PEM")
	}
}

func TestComputeCertFingerprint(t *testing.T) {
	db := setupTLSTestDB(t)
	defer func() { _ = db.Close() }()

	cm := NewCertificateManager(db)
	_ = cm.GenerateSelfSignedCert("test-device")

	var certPEM string
	_ = db.QueryRow(`SELECT value FROM config WHERE key='tls_cert'`).Scan(&certPEM)

	block, _ := pem.Decode([]byte(certPEM))
	cert, _ := x509.ParseCertificate(block.Bytes)

	fp := ComputeCertFingerprint(cert)

	storedFP, _ := cm.GetFingerprint()

	if fp != storedFP {
		t.Errorf("ComputeCertFingerprint mismatch: got %s, want %s", fp, storedFP)
	}
}

func TestNewCertificateManager(t *testing.T) {
	db := setupTLSTestDB(t)
	defer func() { _ = db.Close() }()

	cm := NewCertificateManager(db)
	if cm == nil {
		t.Fatal("NewCertificateManager returned nil")
	}
	if cm.db != db {
		t.Error("db not set correctly")
	}
}

func TestVerifyPeerCertNoCerts(t *testing.T) {
	db := setupTLSTestDB(t)
	defer func() { _ = db.Close() }()

	cm := NewCertificateManager(db)

	err := cm.verifyPeerCert([][]byte{}, nil)
	if err == nil {
		t.Error("expected error for no certificate")
	}
}

func TestVerifyPeerCertUnknownPeer(t *testing.T) {
	db := setupTLSTestDB(t)
	defer func() { _ = db.Close() }()

	cm := NewCertificateManager(db)
	_ = cm.GenerateSelfSignedCert("test-device")

	var certPEM string
	_ = db.QueryRow(`SELECT value FROM config WHERE key='tls_cert'`).Scan(&certPEM)
	block, _ := pem.Decode([]byte(certPEM))

	err := cm.verifyPeerCert([][]byte{block.Bytes}, nil)
	if err == nil {
		t.Error("expected error for unknown peer")
	}
}

func TestVerifyPeerCertFingerprintMismatch(t *testing.T) {
	db := setupTLSTestDB(t)
	defer func() { _ = db.Close() }()

	cm := NewCertificateManager(db)
	_ = cm.GenerateSelfSignedCert("test-device")

	_ = cm.SavePeerCertificate("test-device", "wrongfingerprintwrongfingerprintwrongfingerprintwrongfingerpri")

	var certPEM string
	_ = db.QueryRow(`SELECT value FROM config WHERE key='tls_cert'`).Scan(&certPEM)
	block, _ := pem.Decode([]byte(certPEM))

	err := cm.verifyPeerCert([][]byte{block.Bytes}, nil)
	if err == nil {
		t.Error("expected error for fingerprint mismatch")
	}
}

func TestVerifyPeerCertSuccess(t *testing.T) {
	db := setupTLSTestDB(t)
	defer func() { _ = db.Close() }()

	cm := NewCertificateManager(db)
	_ = cm.GenerateSelfSignedCert("test-device")

	fp, _ := cm.GetFingerprint()
	_ = cm.SavePeerCertificate("test-device", fp)

	var certPEM string
	_ = db.QueryRow(`SELECT value FROM config WHERE key='tls_cert'`).Scan(&certPEM)
	block, _ := pem.Decode([]byte(certPEM))

	err := cm.verifyPeerCert([][]byte{block.Bytes}, nil)
	if err != nil {
		t.Errorf("verifyPeerCert failed: %v", err)
	}
}
