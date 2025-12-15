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
)

type CertificateManager struct {
	db *sql.DB
}

func NewCertificateManager(db *sql.DB) *CertificateManager {
	return &CertificateManager{db: db}
}

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

	_, err = cm.db.Exec(`
		INSERT OR REPLACE INTO config (key, value) VALUES
		('tls_cert', ?),
		('tls_key', ?),
		('tls_fingerprint', ?)
	`, string(certPEM), string(keyPEM), fingerprintHex)

	return err
}

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

func (cm *CertificateManager) SavePeerCertificate(peerID, fingerprint string) error {
	_, err := cm.db.Exec(`
		INSERT OR REPLACE INTO peer_certificates (device_id, fingerprint)
		VALUES (?, ?)
	`, peerID, fingerprint)
	return err
}

func (cm *CertificateManager) GetFingerprint() (string, error) {
	var fingerprint string
	err := cm.db.QueryRow(`
		SELECT value FROM config WHERE key='tls_fingerprint'
	`).Scan(&fingerprint)
	return fingerprint, err
}
