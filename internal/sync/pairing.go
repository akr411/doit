package sync

import (
	"crypto/rand"
	"database/sql"
	"fmt"
	"math/big"
	"time"
)

// PairingCode represents a temporary 6-digit code for device pairing.
// Codes expire after 15 minutes and are single-use for security.
type PairingCode struct {
	Code      string
	CreatedAt int64
	ExpiresAt int64
	Used      bool
}

// PairingManager handles pairing code generation and validation.
// It manages the pairing_codes table and enforces expiration/single-use policies.
type PairingManager struct {
	db     *sql.DB
	secret string
}

// NewPairingManager creates a pairing manager for the given database and shared secret.
func NewPairingManager(db *sql.DB, secret string) *PairingManager {
	return &PairingManager{
		db:     db,
		secret: secret,
	}
}

// GenerateCode creates a new 6-digit pairing code in format "XXX-XXX".
// The code expires after 15 minutes and is stored in the pairing_codes table.
// Returns the generated code or error if random generation/database insert fails.
func (pm *PairingManager) GenerateCode() (*PairingCode, error) {
	max := big.NewInt(1000000)
	n, err := rand.Int(rand.Reader, max)
	if err != nil {
		return nil, err
	}

	code := fmt.Sprintf("%06d", n.Int64())
	codeFormatted := fmt.Sprintf("%s-%s", code[:3], code[3:])

	now := time.Now().UnixNano()
	expiresAt := now + (15 * time.Minute).Nanoseconds()

	pc := &PairingCode{
		Code:      codeFormatted,
		CreatedAt: now,
		ExpiresAt: expiresAt,
		Used:      false,
	}

	_, err = pm.db.Exec(`
		INSERT INTO pairing_codes (code, created_at, expires_at, used)
		VALUES (?, ?, ?, ?)
	`, code, now, expiresAt, false)

	return pc, err
}

// ValidateCode checks if a pairing code is valid, not expired, and not used.
// The code must be in format "XXX-XXX" (7 characters with hyphen at position 3).
// Returns (true, nil) if valid, (false, error) otherwise with specific error message.
func (pm *PairingManager) ValidateCode(code string) (bool, error) {
	if len(code) != 7 {
		return false, fmt.Errorf("invalid code format")
	}

	cleanCode := code[:3] + code[4:]

	var expiresAt int64
	var used bool

	err := pm.db.QueryRow(`
		SELECT expires_at, used
		FROM pairing_codes
		WHERE code = ?
	`, cleanCode).Scan(&expiresAt, &used)

	if err == sql.ErrNoRows {
		return false, fmt.Errorf("invalid code")
	}
	if err != nil {
		return false, err
	}

	if used {
		return false, fmt.Errorf("code already used")
	}

	if time.Now().UnixNano() > expiresAt {
		return false, fmt.Errorf("code expired")
	}

	return true, nil
}

// MarkUsed marks a pairing code as used, preventing reuse.
// The code format is validated before marking.
// Returns error if format is invalid or database update fails.
func (pm *PairingManager) MarkUsed(code string) error {
	if len(code) != 7 {
		return fmt.Errorf("invalid code format")
	}

	cleanCode := code[:3] + code[4:]
	_, err := pm.db.Exec(`
		UPDATE pairing_codes SET used = 1 WHERE code = ?
	`, cleanCode)
	return err
}

// CleanupExpired removes expired pairing codes from the database.
// Should be called periodically to prevent table growth.
// Deletes all codes where expires_at < current time.
func (pm *PairingManager) CleanupExpired() error {
	now := time.Now().UnixNano()
	_, err := pm.db.Exec(`
		DELETE FROM pairing_codes WHERE expires_at < ?
	`, now)
	return err
}
