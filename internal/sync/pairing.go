package sync

import (
	"crypto/rand"
	"database/sql"
	"fmt"
	"math/big"
	"time"
)

type PairingCode struct {
	Code      string
	CreatedAt int64
	ExpiresAt int64
	Used      bool
}

type PairingManager struct {
	db     *sql.DB
	secret string
}

func NewPairingManager(db *sql.DB, secret string) *PairingManager {
	return &PairingManager{
		db:     db,
		secret: secret,
	}
}

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

func (pm *PairingManager) ValidateCode(code string) (bool, error) {
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

func (pm *PairingManager) MarkUsed(code string) error {
	cleanCode := code[:3] + code[4:]
	_, err := pm.db.Exec(`
		UPDATE pairing_codes SET used = 1 WHERE code = ?
	`, cleanCode)
	return err
}

func (pm *PairingManager) CleanupExpired() error {
	now := time.Now().UnixNano()
	_, err := pm.db.Exec(`
		DELETE FROM pairing_codes WHERE expires_at < ?
	`, now)
	return err
}
