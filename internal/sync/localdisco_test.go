package sync

import (
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

type mockPeerManager struct {
	peers []*Peer
}

func (m *mockPeerManager) AddOrUpdatePeer(peer *Peer) error {
	m.peers = append(m.peers, peer)
	return nil
}

type mockStore struct {
	db *sql.DB
}

func (m *mockStore) GetDB() *sql.DB {
	return m.db
}

func setupLocalDiscoTestDB(t *testing.T) *sql.DB {
	t.Helper()
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("failed to open db: %v", err)
	}

	schema := `
	CREATE TABLE IF NOT EXISTS config (
		key TEXT PRIMARY KEY,
		value TEXT NOT NULL
	);
	`

	if _, err := db.Exec(schema); err != nil {
		t.Fatalf("failed to create schema: %v", err)
	}

	return db
}

func TestNewLocalDiscovery(t *testing.T) {
	db := setupLocalDiscoTestDB(t)
	defer func() { _ = db.Close() }()

	store := &mockStore{db: db}
	peerMgr := &mockPeerManager{}

	ld := NewLocalDiscovery(store, peerMgr)
	if ld == nil {
		t.Fatal("NewLocalDiscovery returned nil")
	}

	if ld.store != store {
		t.Error("store not set correctly")
	}

	if ld.peerMgr != peerMgr {
		t.Error("peerMgr not set correctly")
	}
}

func TestLocalDiscoveryStartStop(t *testing.T) {
	db := setupLocalDiscoTestDB(t)
	defer func() { _ = db.Close() }()

	_, _ = db.Exec("INSERT INTO config (key, value) VALUES ('device_id', 'test-device-id-1234567890123456')")

	store := &mockStore{db: db}
	peerMgr := &mockPeerManager{}

	ld := NewLocalDiscovery(store, peerMgr)

	err := ld.Start(49152)
	if err != nil {
		t.Logf("Start may fail in test env (port binding): %v", err)
		return
	}

	time.Sleep(50 * time.Millisecond)

	err = ld.Stop()
	if err != nil {
		t.Errorf("Stop failed: %v", err)
	}

	err = ld.Stop()
	if err != nil {
		t.Error("Stop should be idempotent")
	}
}

func TestAnnouncementPacketMarshal(t *testing.T) {
	tests := []struct {
		name    string
		packet  *AnnouncementPacket
		wantErr bool
	}{
		{
			name: "valid packet",
			packet: &AnnouncementPacket{
				Magic:    MagicNumber,
				DeviceID: "12345678-1234-1234-1234-123456789012",
				Port:     49152,
				Name:     "TestDevice",
			},
			wantErr: false,
		},
		{
			name: "device ID too short",
			packet: &AnnouncementPacket{
				Magic:    MagicNumber,
				DeviceID: "short",
				Port:     49152,
				Name:     "TestDevice",
			},
			wantErr: true,
		},
		{
			name: "device ID too long",
			packet: &AnnouncementPacket{
				Magic:    MagicNumber,
				DeviceID: "12345678-1234-1234-1234-1234567890123",
				Port:     49152,
				Name:     "TestDevice",
			},
			wantErr: true,
		},
		{
			name: "empty name",
			packet: &AnnouncementPacket{
				Magic:    MagicNumber,
				DeviceID: "12345678-1234-1234-1234-123456789012",
				Port:     49152,
				Name:     "",
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := tt.packet.Marshal()
			if (err != nil) != tt.wantErr {
				t.Errorf("Marshal() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestAnnouncementPacketMarshalNameTooLong(t *testing.T) {
	longName := make([]byte, 256)
	for i := range longName {
		longName[i] = 'a'
	}

	packet := &AnnouncementPacket{
		Magic:    MagicNumber,
		DeviceID: "12345678-1234-1234-1234-123456789012",
		Port:     49152,
		Name:     string(longName),
	}

	_, err := packet.Marshal()
	if err == nil {
		t.Error("expected error for name > 255 chars")
	}
}

func TestUnmarshalAnnouncement(t *testing.T) {
	originalPacket := &AnnouncementPacket{
		Magic:    MagicNumber,
		DeviceID: "12345678-1234-1234-1234-123456789012",
		Port:     49152,
		Name:     "TestDevice",
	}

	data, err := originalPacket.Marshal()
	if err != nil {
		t.Fatalf("Marshal failed: %v", err)
	}

	parsed, err := UnmarshalAnnouncement(data)
	if err != nil {
		t.Fatalf("UnmarshalAnnouncement failed: %v", err)
	}

	if parsed.Magic != originalPacket.Magic {
		t.Errorf("Magic mismatch: got %x, want %x", parsed.Magic, originalPacket.Magic)
	}

	if parsed.DeviceID != originalPacket.DeviceID {
		t.Errorf("DeviceID mismatch: got %s, want %s", parsed.DeviceID, originalPacket.DeviceID)
	}

	if parsed.Port != originalPacket.Port {
		t.Errorf("Port mismatch: got %d, want %d", parsed.Port, originalPacket.Port)
	}

	if parsed.Name != originalPacket.Name {
		t.Errorf("Name mismatch: got %s, want %s", parsed.Name, originalPacket.Name)
	}
}

func TestUnmarshalAnnouncementErrors(t *testing.T) {
	tests := []struct {
		name    string
		data    []byte
		wantErr string
	}{
		{
			name:    "packet too small",
			data:    make([]byte, 42),
			wantErr: "packet too small",
		},
		{
			name:    "invalid magic number",
			data:    append([]byte{0x00, 0x00, 0x00, 0x00}, make([]byte, 50)...),
			wantErr: "invalid magic number",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := UnmarshalAnnouncement(tt.data)
			if err == nil {
				t.Error("expected error")
			}
		})
	}
}

func TestMarshalUnmarshalRoundtrip(t *testing.T) {
	testCases := []struct {
		name     string
		deviceID string
		port     uint16
		devName  string
	}{
		{
			name:     "standard",
			deviceID: "12345678-1234-1234-1234-123456789012",
			port:     49152,
			devName:  "MyDevice",
		},
		{
			name:     "max port",
			deviceID: "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee",
			port:     65535,
			devName:  "MaxPort",
		},
		{
			name:     "min port",
			deviceID: "00000000-0000-0000-0000-000000000000",
			port:     1,
			devName:  "MinPort",
		},
		{
			name:     "short name",
			deviceID: "12345678-1234-1234-1234-123456789012",
			port:     8080,
			devName:  "A",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			original := &AnnouncementPacket{
				Magic:    MagicNumber,
				DeviceID: tc.deviceID,
				Port:     tc.port,
				Name:     tc.devName,
			}

			data, err := original.Marshal()
			if err != nil {
				t.Fatalf("Marshal failed: %v", err)
			}

			parsed, err := UnmarshalAnnouncement(data)
			if err != nil {
				t.Fatalf("Unmarshal failed: %v", err)
			}

			if parsed.DeviceID != original.DeviceID {
				t.Errorf("DeviceID mismatch")
			}
			if parsed.Port != original.Port {
				t.Errorf("Port mismatch")
			}
			if parsed.Name != original.Name {
				t.Errorf("Name mismatch")
			}
		})
	}
}

func TestMagicNumberConstant(t *testing.T) {
	if MagicNumber != 0xD01744A7 {
		t.Errorf("MagicNumber = %x, want 0xD01744A7", MagicNumber)
	}
}

func TestDiscoveryPortConstant(t *testing.T) {
	if DiscoveryPort != 49151 {
		t.Errorf("DiscoveryPort = %d, want 49151", DiscoveryPort)
	}
}
