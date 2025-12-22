package sync

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"database/sql"
	"encoding/hex"
	"encoding/pem"
	"fmt"
	"math/big"
	"time"

	"github.com/akr411/doit/internal/keyring"
)

// CertificateManager handles TLS certificate generation, storage, and verification.
// It manages Ed25519 self-signed certificates with SHA256 fingerprint pinning for mTLS.
type CertificateManager struct {
	db *sql.DB
}

// NewCertificateManager creates a certificate manager for the given database.
func NewCertificateManager(db *sql.DB) *CertificateManager {
	return &CertificateManager{db: db}
}

// GenerateSelfSignedCert creates an Ed25519 self-signed certificate for the device.
// The certificate is valid for 10 years and includes the device ID in the Common Name.
// Stores the certificate, private key, and SHA256 fingerprint in config table.
// Returns nil if certificate already exists. Returns error if generation or storage fails.
func (cm *CertificateManager) GenerateSelfSignedCert(deviceID string) error {
	var exists bool
	err := cm.db.QueryRow(`
		SELECT EXISTS(SELECT 1 FROM config WHERE key='tls_cert')
	`).Scan(&exists)

	if err == nil && exists {
		return nil
	}

	pubKey, privKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return err
	}

	serialNumber, _ := rand.Int(rand.Reader, big.NewInt(1<<62))
	template := &x509.Certificate{
		SerialNumber: serialNumber,
		Subject: pkix.Name{
			CommonName: deviceID,
		},
		NotBefore:             time.Now(),
		NotAfter:              time.Now().Add(10 * 365 * 24 * time.Hour),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth, x509.ExtKeyUsageClientAuth},
		BasicConstraintsValid: true,
	}

	certDER, err := x509.CreateCertificate(rand.Reader, template, template, pubKey, privKey)
	if err != nil {
		return err
	}

	certPEM := pem.EncodeToMemory(&pem.Block{
		Type:  "CERTIFICATE",
		Bytes: certDER,
	})

	privBytes, err := x509.MarshalPKCS8PrivateKey(privKey)
	if err != nil {
		return err
	}

	keyPEM := pem.EncodeToMemory(&pem.Block{
		Type:  "PRIVATE KEY",
		Bytes: privBytes,
	})

	fingerprint := sha256.Sum256(certDER)
	fingerprintHex := hex.EncodeToString(fingerprint[:])

	keyStorage := string(keyPEM)
	if keyring.IsAvailable() {
		if err := keyring.SetTLSKey(deviceID, string(keyPEM)); err == nil {
			keyStorage = "[keyring]"
		}
	}

	_, err = cm.db.Exec(`
		INSERT OR REPLACE INTO config (key, value) VALUES
		('tls_cert', ?),
		('tls_key', ?),
		('tls_fingerprint', ?)
	`, string(certPEM), keyStorage, fingerprintHex)

	return err
}

// GetTLSConfig returns a TLS configuration for server or client use.
// Server config enforces mTLS with RequireAnyClientCert and peer certificate verification.
// Client config skips hostname verification but verifies peer certificate fingerprint.
// Both configs use TLS 1.3 with AES-256-GCM and ChaCha20-Poly1305 ciphers.
// Returns error if certificate not found in config.
func (cm *CertificateManager) GetTLSConfig(isServer bool) (*tls.Config, error) {
	var certPEM, keyPEM string
	err := cm.db.QueryRow(`
		SELECT
			(SELECT value FROM config WHERE key='tls_cert'),
			(SELECT value FROM config WHERE key='tls_key')
	`).Scan(&certPEM, &keyPEM)

	if err != nil {
		return nil, fmt.Errorf("TLS cert not found. Run 'doit sync init' to generate")
	}

	if keyPEM == "[keyring]" && keyring.IsAvailable() {
		var deviceID string
		_ = cm.db.QueryRow("SELECT value FROM config WHERE key='device_id'").Scan(&deviceID)
		if deviceID != "" {
			if key, err := keyring.GetTLSKey(deviceID); err == nil {
				keyPEM = key
			}
		}
	}

	cert, err := tls.X509KeyPair([]byte(certPEM), []byte(keyPEM))
	if err != nil {
		return nil, err
	}

	config := &tls.Config{
		Certificates: []tls.Certificate{cert},
		MinVersion:   tls.VersionTLS13,
		CipherSuites: []uint16{
			tls.TLS_AES_256_GCM_SHA384,
			tls.TLS_CHACHA20_POLY1305_SHA256,
		},
	}

	if isServer {
		config.ClientAuth = tls.RequireAnyClientCert
		config.VerifyPeerCertificate = cm.verifyPeerCert
	} else {
		config.InsecureSkipVerify = true
		config.VerifyPeerCertificate = cm.verifyPeerCert
	}

	return config, nil
}

func (cm *CertificateManager) verifyPeerCert(rawCerts [][]byte, verifiedChains [][]*x509.Certificate) error {
	if len(rawCerts) == 0 {
		return fmt.Errorf("no certificate provided")
	}

	fingerprint := sha256.Sum256(rawCerts[0])
	fingerprintHex := hex.EncodeToString(fingerprint[:])

	cert, err := x509.ParseCertificate(rawCerts[0])
	if err != nil {
		return err
	}

	now := time.Now()
	if now.Before(cert.NotBefore) {
		return fmt.Errorf("certificate not yet valid (starts %s)", cert.NotBefore.Format(time.RFC3339))
	}
	if now.After(cert.NotAfter) {
		return fmt.Errorf("certificate expired (expired %s)", cert.NotAfter.Format(time.RFC3339))
	}

	var storedFingerprint string
	err = cm.db.QueryRow(`
		SELECT fingerprint FROM peer_certificates
		WHERE device_id = ?
	`, cert.Subject.CommonName).Scan(&storedFingerprint)

	if err == sql.ErrNoRows {
		return fmt.Errorf("unknown peer: %s", cert.Subject.CommonName)
	}
	if err != nil {
		return err
	}

	if storedFingerprint != fingerprintHex {
		return fmt.Errorf("certificate fingerprint mismatch")
	}

	return nil
}

// SavePeerCertificate stores a peer's certificate fingerprint for verification.
// The fingerprint is saved in peer_certificates table keyed by device ID.
// Used during pairing to enable subsequent mTLS verification.
func (cm *CertificateManager) SavePeerCertificate(peerID, fingerprint string) error {
	_, err := cm.db.Exec(`
		INSERT OR REPLACE INTO peer_certificates (device_id, fingerprint)
		VALUES (?, ?)
	`, peerID, fingerprint)
	return err
}

// GetFingerprint retrieves this device's TLS certificate SHA256 fingerprint.
// Returns the 64-character hex string or error if not found in config.
func (cm *CertificateManager) GetFingerprint() (string, error) {
	var fingerprint string
	err := cm.db.QueryRow(`
		SELECT value FROM config WHERE key='tls_fingerprint'
	`).Scan(&fingerprint)
	return fingerprint, err
}

// ComputeCertFingerprint computes the SHA256 fingerprint of a certificate.
// Returns the 64-character hex-encoded fingerprint string.
func ComputeCertFingerprint(cert *x509.Certificate) string {
	fingerprint := sha256.Sum256(cert.Raw)
	return hex.EncodeToString(fingerprint[:])
}

// CheckCertValidity checks if local certificate is valid and not expiring soon.
// Returns nil if valid, error describing the issue otherwise.
// Warns if expiring within 30 days.
func (cm *CertificateManager) CheckCertValidity() error {
	var certPEM string
	err := cm.db.QueryRow(`SELECT value FROM config WHERE key='tls_cert'`).Scan(&certPEM)
	if err != nil {
		return fmt.Errorf("no certificate found")
	}

	block, _ := pem.Decode([]byte(certPEM))
	if block == nil {
		return fmt.Errorf("failed to parse certificate PEM")
	}

	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return fmt.Errorf("failed to parse certificate: %w", err)
	}

	now := time.Now()
	if now.After(cert.NotAfter) {
		return fmt.Errorf("certificate expired on %s", cert.NotAfter.Format("2006-01-02"))
	}

	daysUntilExpiry := int(cert.NotAfter.Sub(now).Hours() / 24)
	if daysUntilExpiry < 30 {
		return fmt.Errorf("certificate expires in %d days (on %s)", daysUntilExpiry, cert.NotAfter.Format("2006-01-02"))
	}

	return nil
}

// RegenerateCertIfExpired checks cert validity and regenerates if expired.
// Returns true if regenerated, false if still valid.
func (cm *CertificateManager) RegenerateCertIfExpired(deviceID string) (bool, error) {
	var certPEM string
	err := cm.db.QueryRow(`SELECT value FROM config WHERE key='tls_cert'`).Scan(&certPEM)
	if err == sql.ErrNoRows {
		if err := cm.GenerateSelfSignedCert(deviceID); err != nil {
			return false, err
		}
		return true, nil
	}
	if err != nil {
		return false, err
	}

	block, _ := pem.Decode([]byte(certPEM))
	if block == nil {
		_, _ = cm.db.Exec(`DELETE FROM config WHERE key IN ('tls_cert', 'tls_key', 'tls_fingerprint')`)
		if err := cm.GenerateSelfSignedCert(deviceID); err != nil {
			return false, err
		}
		return true, nil
	}

	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		_, _ = cm.db.Exec(`DELETE FROM config WHERE key IN ('tls_cert', 'tls_key', 'tls_fingerprint')`)
		if err := cm.GenerateSelfSignedCert(deviceID); err != nil {
			return false, err
		}
		return true, nil
	}

	if time.Now().After(cert.NotAfter) {
		_, _ = cm.db.Exec(`DELETE FROM config WHERE key IN ('tls_cert', 'tls_key', 'tls_fingerprint')`)
		if err := cm.GenerateSelfSignedCert(deviceID); err != nil {
			return false, err
		}
		return true, nil
	}

	return false, nil
}
