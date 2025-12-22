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
	defer func() { _ = db.Close() }()

	pm := NewPairingManager(db, "test-secret")

	code, err := pm.GenerateCode()
	if err != nil {
		t.Fatal(err)
	}

	if len(code.Code) != 9 {
		t.Errorf("Expected code length 9, got %d", len(code.Code))
	}

	if code.Code[4] != '-' {
		t.Error("Expected hyphen at position 4")
	}

	expiresIn := time.Until(time.Unix(0, code.ExpiresAt))
	expectedExpiry := 5 * time.Minute
	if expiresIn < 4*time.Minute || expiresIn > expectedExpiry {
		t.Errorf("Expected expiry ~5min, got %v", expiresIn)
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
			code:    "12345678",
			wantOK:  false,
			wantErr: "invalid code format",
		},
		{
			name:    "non-existent code",
			code:    "9999-9999",
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
				`, "12345678", expired, expired+5*time.Minute.Nanoseconds(), 0)
				if err != nil {
					t.Fatal(err)
				}
				return "1234-5678"
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
				_, _ = pm.ValidateAndConsumeCode(code.Code)
				return code.Code
			},
			wantOK:  false,
			wantErr: "code already used",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db := setupPairingTestDB(t)
			defer func() { _ = db.Close() }()

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

func TestConsumedCodeBecomesInvalid(t *testing.T) {
	db := setupPairingTestDB(t)
	defer func() { _ = db.Close() }()

	pm := NewPairingManager(db, "test-secret")

	code, err := pm.GenerateCode()
	if err != nil {
		t.Fatal(err)
	}

	ok, err := pm.ValidateAndConsumeCode(code.Code)
	if !ok || err != nil {
		t.Fatalf("ValidateAndConsumeCode() failed: %v", err)
	}

	ok, err = pm.ValidateCode(code.Code)
	if ok {
		t.Error("Consumed code should not validate")
	}
	if err == nil || err.Error() != "code already used" {
		t.Errorf("Expected 'code already used' error, got %v", err)
	}
}

func TestCleanupExpired(t *testing.T) {
	db := setupPairingTestDB(t)
	defer func() { _ = db.Close() }()

	pm := NewPairingManager(db, "test-secret")

	expired := time.Now().UnixNano() - 30*time.Minute.Nanoseconds()
	_, _ = db.Exec(`
		INSERT INTO pairing_codes (code, created_at, expires_at, used)
		VALUES (?, ?, ?, ?)
	`, "111111", expired, expired+15*time.Minute.Nanoseconds(), 0)

	code, _ := pm.GenerateCode()

	if err := pm.CleanupExpired(); err != nil {
		t.Fatal(err)
	}

	var count int
	_ = db.QueryRow("SELECT COUNT(*) FROM pairing_codes").Scan(&count)

	if count != 1 {
		t.Errorf("Expected 1 code after cleanup (fresh code), got %d", count)
	}

	_, err := pm.ValidateCode(code.Code)
	if err != nil {
		t.Error("Fresh code should still be valid after cleanup")
	}
}

func TestValidateAndConsumeCode(t *testing.T) {
	tests := []struct {
		name    string
		setup   func(*testing.T, *PairingManager) string
		wantOK  bool
		wantErr string
	}{
		{
			name: "valid code consumed atomically",
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
			name: "code already used",
			setup: func(t *testing.T, pm *PairingManager) string {
				code, err := pm.GenerateCode()
				if err != nil {
					t.Fatal(err)
				}
				_, _ = pm.ValidateAndConsumeCode(code.Code)
				return code.Code
			},
			wantOK:  false,
			wantErr: "code already used",
		},
		{
			name: "expired code",
			setup: func(t *testing.T, pm *PairingManager) string {
				now := time.Now().UnixNano()
				expired := now - 20*time.Minute.Nanoseconds()
				_, err := pm.db.Exec(`
					INSERT INTO pairing_codes (code, created_at, expires_at, used)
					VALUES (?, ?, ?, ?)
				`, "99998888", expired, expired+5*time.Minute.Nanoseconds(), 0)
				if err != nil {
					t.Fatal(err)
				}
				return "9999-8888"
			},
			wantOK:  false,
			wantErr: "code expired",
		},
		{
			name: "non-existent code",
			setup: func(t *testing.T, pm *PairingManager) string {
				return "0000-0000"
			},
			wantOK:  false,
			wantErr: "invalid code",
		},
		{
			name: "invalid format",
			setup: func(t *testing.T, pm *PairingManager) string {
				return "123"
			},
			wantOK:  false,
			wantErr: "invalid code format",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db := setupPairingTestDB(t)
			defer func() { _ = db.Close() }()

			pm := NewPairingManager(db, "test-secret")

			code := tt.setup(t, pm)

			ok, err := pm.ValidateAndConsumeCode(code)
			if ok != tt.wantOK {
				t.Errorf("ValidateAndConsumeCode() ok = %v, want %v", ok, tt.wantOK)
			}

			if !tt.wantOK && err != nil {
				if err.Error() != tt.wantErr {
					t.Errorf("ValidateAndConsumeCode() err = %v, want %v", err.Error(), tt.wantErr)
				}
			}

			if tt.wantOK {
				_, err := pm.ValidateCode(code)
				if err == nil || err.Error() != "code already used" {
					t.Error("code should be marked as used after consumption")
				}
			}
		})
	}
}

func TestCleanPairingCodeEdgeCases(t *testing.T) {
	tests := []struct {
		name    string
		code    string
		want    string
		wantErr bool
	}{
		{
			name:    "valid format",
			code:    "1234-5678",
			want:    "12345678",
			wantErr: false,
		},
		{
			name:    "too short",
			code:    "123-456",
			want:    "",
			wantErr: true,
		},
		{
			name:    "too long",
			code:    "12345-67890",
			want:    "",
			wantErr: true,
		},
		{
			name:    "no hyphen",
			code:    "123456789",
			want:    "",
			wantErr: true,
		},
		{
			name:    "hyphen wrong position",
			code:    "123-45678",
			want:    "",
			wantErr: true,
		},
		{
			name:    "empty string",
			code:    "",
			want:    "",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := cleanPairingCode(tt.code)
			if (err != nil) != tt.wantErr {
				t.Errorf("cleanPairingCode() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if got != tt.want {
				t.Errorf("cleanPairingCode() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestValidateAndConsumeCodeAtomicity(t *testing.T) {
	db := setupPairingTestDB(t)
	defer func() { _ = db.Close() }()

	pm := NewPairingManager(db, "test-secret")

	code, err := pm.GenerateCode()
	if err != nil {
		t.Fatal(err)
	}

	successCount := 0
	done := make(chan bool, 10)

	for i := 0; i < 10; i++ {
		go func() {
			ok, _ := pm.ValidateAndConsumeCode(code.Code)
			if ok {
				done <- true
			} else {
				done <- false
			}
		}()
	}

	for i := 0; i < 10; i++ {
		if <-done {
			successCount++
		}
	}

	if successCount != 1 {
		t.Errorf("Expected exactly 1 successful consumption, got %d", successCount)
	}
}
