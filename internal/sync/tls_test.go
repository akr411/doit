package sync

import (
	"crypto/tls"
	"database/sql"
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
	defer db.Close()

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
	defer db.Close()

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
			defer db.Close()

			cm := NewCertificateManager(db)
			cm.GenerateSelfSignedCert("device-test")

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
	defer db.Close()

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
