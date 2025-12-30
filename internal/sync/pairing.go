package sync

import (
	"crypto/rand"
	"crypto/subtle"
	"database/sql"
	"fmt"
	"math/big"
	"time"
)

// PairingCode represents a temporary 8-digit code (XXXX-XXXX format) for device pairing.
// Codes expire after 5 minutes and are single-use for security.
type PairingCode struct {
	Code      string
	CreatedAt int64
	ExpiresAt int64
	Used      bool
}

// PairingManager handles pairing code generation and validation.
// It manages the pairing_codes table and enforces expiration/single-use policies.
type PairingManager struct {
	db *sql.DB
}

// querier abstracts database query operations for both *sql.DB and *sql.Tx.
type querier interface {
	QueryRow(query string, args ...any) *sql.Row
}

// NewPairingManager creates a pairing manager for the given database.
func NewPairingManager(db *sql.DB) *PairingManager {
	return &PairingManager{
		db: db,
	}
}

// GenerateCode creates a new 8-digit pairing code in format "XXXX-XXXX".
// The code expires after 5 minutes and is stored in the pairing_codes table.
// Returns the generated code or error if random generation/database insert fails.
func (pm *PairingManager) GenerateCode() (*PairingCode, error) {
	max := big.NewInt(100000000)
	n, err := rand.Int(rand.Reader, max)
	if err != nil {
		return nil, fmt.Errorf("failed to generate random code: %w", err)
	}

	code := fmt.Sprintf("%08d", n.Int64())
	codeFormatted := fmt.Sprintf("%s-%s", code[:4], code[4:])

	now := time.Now().UnixNano()
	expiresAt := now + (5 * time.Minute).Nanoseconds()

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
// The code must be in format "XXXX-XXXX" (9 characters with hyphen at position 4).
// Returns (true, nil) if valid, (false, error) otherwise with specific error message.
// Note: This only validates; use ValidateAndConsumeCode for atomic validation+consumption.
func (pm *PairingManager) ValidateCode(code string) (bool, error) {
	cleanCode, err := cleanPairingCode(code)
	if err != nil {
		return false, err
	}
	return validateCodeWithQuerier(pm.db, cleanCode)
}

// ValidateAndConsumeCode atomically validates and marks a pairing code as used.
// This prevents race conditions where two requests could use the same code.
// Returns (true, nil) if code was valid and consumed, (false, error) otherwise.
func (pm *PairingManager) ValidateAndConsumeCode(code string) (bool, error) {
	cleanCode, err := cleanPairingCode(code)
	if err != nil {
		return false, err
	}

	tx, err := pm.db.Begin()
	if err != nil {
		return false, fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	valid, err := validateCodeWithQuerier(tx, cleanCode)
	if !valid || err != nil {
		return false, err
	}

	_, err = tx.Exec(`UPDATE pairing_codes SET used = 1 WHERE code = ?`, cleanCode)
	if err != nil {
		return false, fmt.Errorf("failed to mark code used: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return false, fmt.Errorf("failed to commit: %w", err)
	}

	return true, nil
}

// validateCodeWithQuerier performs code validation using the provided querier (DB or Tx).
func validateCodeWithQuerier(q querier, cleanCode string) (bool, error) {
	var storedCode string
	var expiresAt int64
	var used bool

	err := q.QueryRow(`
		SELECT code, expires_at, used
		FROM pairing_codes
		WHERE code = ?
	`, cleanCode).Scan(&storedCode, &expiresAt, &used)

	if err == sql.ErrNoRows {
		return false, fmt.Errorf("invalid code")
	}
	if err != nil {
		return false, err
	}

	if subtle.ConstantTimeCompare([]byte(cleanCode), []byte(storedCode)) != 1 {
		return false, fmt.Errorf("invalid code")
	}

	if used {
		return false, fmt.Errorf("code already used")
	}

	if time.Now().UnixNano() > expiresAt {
		return false, fmt.Errorf("code expired")
	}

	return true, nil
}

func cleanPairingCode(code string) (string, error) {
	if len(code) != 9 || code[4] != '-' {
		return "", fmt.Errorf("invalid code format")
	}
	return code[:4] + code[5:], nil
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
