package sync

import (
	"database/sql"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

func setupPairingTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}

	schema := `
	CREATE TABLE config (key TEXT PRIMARY KEY, value TEXT NOT NULL);
	CREATE TABLE pairing_codes (
		code TEXT PRIMARY KEY,
		created_at INTEGER NOT NULL,
		expires_at INTEGER NOT NULL,
		used INTEGER DEFAULT 0
	);
	CREATE INDEX idx_pairing_expires ON pairing_codes(expires_at);
	`

	if _, err := db.Exec(schema); err != nil {
		t.Fatal(err)
	}

	return db
}

func TestGenerateCode(t *testing.T) {
	db := setupPairingTestDB(t)
	defer db.Close()

	pm := NewPairingManager(db, "test-secret")

	code, err := pm.GenerateCode()
	if err != nil {
		t.Fatal(err)
	}

	if len(code.Code) != 7 {
		t.Errorf("Expected code length 7, got %d", len(code.Code))
	}

	if code.Code[3] != '-' {
		t.Error("Expected hyphen at position 3")
	}

	expiresIn := time.Unix(0, code.ExpiresAt).Sub(time.Now())
	expectedExpiry := 15 * time.Minute
	if expiresIn < 14*time.Minute || expiresIn > expectedExpiry {
		t.Errorf("Expected expiry ~15min, got %v", expiresIn)
	}
}

func TestValidateCodeTable(t *testing.T) {
	tests := []struct {
		name    string
		code    string
		setup   func(*testing.T, *PairingManager) string
		wantOK  bool
		wantErr string
	}{
		{
			name: "valid fresh code",
			setup: func(t *testing.T, pm *PairingManager) string {
				code, err := pm.GenerateCode()
				if err != nil {
					t.Fatal(err)
				}
				return code.Code
			},
			wantOK: true,
		},
		{
			name:    "invalid format too short",
			code:    "123",
			wantOK:  false,
			wantErr: "invalid code format",
		},
		{
			name:    "invalid format no hyphen",
			code:    "123456",
			wantOK:  false,
			wantErr: "invalid code format",
		},
		{
			name:    "non-existent code",
			code:    "999-999",
			wantOK:  false,
			wantErr: "invalid code",
		},
		{
			name: "expired code",
			setup: func(t *testing.T, pm *PairingManager) string {
				now := time.Now().UnixNano()
				expired := now - 20*time.Minute.Nanoseconds()
				_, err := pm.db.Exec(`
					INSERT INTO pairing_codes (code, created_at, expires_at, used)
					VALUES (?, ?, ?, ?)
				`, "123456", expired, expired+15*time.Minute.Nanoseconds(), 0)
				if err != nil {
					t.Fatal(err)
				}
				return "123-456"
			},
			wantOK:  false,
			wantErr: "code expired",
		},
		{
			name: "already used code",
			setup: func(t *testing.T, pm *PairingManager) string {
				code, err := pm.GenerateCode()
				if err != nil {
					t.Fatal(err)
				}
				pm.MarkUsed(code.Code)
				return code.Code
			},
			wantOK:  false,
			wantErr: "code already used",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db := setupPairingTestDB(t)
			defer db.Close()

			pm := NewPairingManager(db, "test-secret")

			code := tt.code
			if tt.setup != nil {
				code = tt.setup(t, pm)
			}

			ok, err := pm.ValidateCode(code)
			if ok != tt.wantOK {
				t.Errorf("ValidateCode() ok = %v, want %v", ok, tt.wantOK)
			}

			if !tt.wantOK && err != nil {
				if err.Error() != tt.wantErr {
					t.Errorf("ValidateCode() err = %v, want %v", err.Error(), tt.wantErr)
				}
			}
		})
	}
}

func TestMarkUsed(t *testing.T) {
	db := setupPairingTestDB(t)
	defer db.Close()

	pm := NewPairingManager(db, "test-secret")

	code, err := pm.GenerateCode()
	if err != nil {
		t.Fatal(err)
	}

	if err := pm.MarkUsed(code.Code); err != nil {
		t.Fatalf("MarkUsed() failed: %v", err)
	}

	ok, err := pm.ValidateCode(code.Code)
	if ok {
		t.Error("Used code should not validate")
	}
	if err == nil || err.Error() != "code already used" {
		t.Errorf("Expected 'code already used' error, got %v", err)
	}
}

func TestCleanupExpired(t *testing.T) {
	db := setupPairingTestDB(t)
	defer db.Close()

	pm := NewPairingManager(db, "test-secret")

	expired := time.Now().UnixNano() - 30*time.Minute.Nanoseconds()
	db.Exec(`
		INSERT INTO pairing_codes (code, created_at, expires_at, used)
		VALUES (?, ?, ?, ?)
	`, "111111", expired, expired+15*time.Minute.Nanoseconds(), 0)

	code, _ := pm.GenerateCode()

	if err := pm.CleanupExpired(); err != nil {
		t.Fatal(err)
	}

	var count int
	db.QueryRow("SELECT COUNT(*) FROM pairing_codes").Scan(&count)

	if count != 1 {
		t.Errorf("Expected 1 code after cleanup (fresh code), got %d", count)
	}

	_, err := pm.ValidateCode(code.Code)
	if err != nil {
		t.Error("Fresh code should still be valid after cleanup")
	}
}
