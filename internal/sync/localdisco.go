package sync

import (
	"bytes"
	"database/sql"
	"encoding/binary"
	"fmt"
	"net"
	"sync"
	"time"

	"github.com/akr411/doit/internal/logging"
)

const (
	DiscoveryPort = 49151
	MagicNumber   = 0xD01744A7
)

// LocalDiscoveryStore defines the storage interface required by local discovery.
type LocalDiscoveryStore interface {
	GetDB() *sql.DB
}

// LocalDiscoveryPeerManager defines the peer management interface for discovery.
type LocalDiscoveryPeerManager interface {
	AddOrUpdatePeer(peer *Peer) error
}

// LocalDiscovery handles UDP broadcast for discovering peers on the local network.
// It broadcasts announcement packets every 10 seconds and listens for peer announcements.
// Supports both IPv4 and IPv6 on port 49151.
type LocalDiscovery struct {
	store   LocalDiscoveryStore
	peerMgr LocalDiscoveryPeerManager
	port    int
	conn4   *net.UDPConn
	conn6   *net.UDPConn
	stopCh  chan struct{}
	mu      sync.Mutex
	running bool
	wg      sync.WaitGroup
}

// AnnouncementPacket is the UDP broadcast message for peer discovery.
// Contains device identification and sync port information.
type AnnouncementPacket struct {
	Magic    uint32
	DeviceID string
	Port     uint16
	Name     string
}

// NewLocalDiscovery creates a local discovery service for the given store and peer manager.
func NewLocalDiscovery(store LocalDiscoveryStore, peerMgr LocalDiscoveryPeerManager) *LocalDiscovery {
	return &LocalDiscovery{
		store:   store,
		peerMgr: peerMgr,
		stopCh:  make(chan struct{}),
	}
}

// Start launches UDP broadcast discovery on IPv4 and IPv6.
// Binds to port 49151, starts announcement and receive loops.
// Returns error if already running or UDP binding fails.
func (ld *LocalDiscovery) Start(port int) error {
	ld.mu.Lock()
	if ld.running {
		ld.mu.Unlock()
		return fmt.Errorf("local discovery already running")
	}
	ld.running = true
	ld.mu.Unlock()

	ld.port = port

	addr4 := &net.UDPAddr{
		IP:   net.IPv4zero,
		Port: DiscoveryPort,
	}
	conn4, err := net.ListenUDP("udp4", addr4)
	if err != nil {
		ld.running = false
		return fmt.Errorf("failed to bind IPv4 UDP: %w", err)
	}

	if err := conn4.SetReadBuffer(1048576); err != nil {
		logging.Warn(" Failed to set read buffer: %v", err)
	}

	ld.conn4 = conn4

	addr6 := &net.UDPAddr{
		IP:   net.IPv6zero,
		Port: DiscoveryPort,
	}
	conn6, err := net.ListenUDP("udp6", addr6)
	if err != nil {
		logging.Warn(" Failed to bind IPv6 UDP: %v", err)
	} else {
		ld.conn6 = conn6
	}

	ld.wg.Add(2)
	go ld.announceLoop()
	go ld.receiveLoop()

	logging.Info(" Local discovery started on port %d", DiscoveryPort)

	return nil
}

// Stop gracefully shuts down local discovery.
// Closes UDP connections and stops announcement/receive loops.
// Safe to call multiple times.
func (ld *LocalDiscovery) Stop() error {
	ld.mu.Lock()
	if !ld.running {
		ld.mu.Unlock()
		return nil
	}
	ld.running = false
	ld.mu.Unlock()

	close(ld.stopCh)

	if ld.conn4 != nil {
		_ = ld.conn4.SetReadDeadline(time.Now())
		_ = ld.conn4.Close()
	}
	if ld.conn6 != nil {
		_ = ld.conn6.SetReadDeadline(time.Now())
		_ = ld.conn6.Close()
	}

	ld.wg.Wait()

	return nil
}

func (ld *LocalDiscovery) announceLoop() {
	defer ld.wg.Done()
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	ld.announce()

	for {
		select {
		case <-ticker.C:
			ld.announce()
		case <-ld.stopCh:
			return
		}
	}
}

func (ld *LocalDiscovery) announce() {
	deviceID, err := GetDeviceID(ld.store.GetDB())
	if err != nil {
		logging.Error(" Failed to get device ID: %v", err)
		return
	}

	deviceName := GetDeviceName()

	packet := &AnnouncementPacket{
		Magic:    MagicNumber,
		DeviceID: deviceID,
		Port:     uint16(ld.port),
		Name:     deviceName,
	}

	data, err := packet.Marshal()
	if err != nil {
		logging.Error(" Failed to marshal packet: %v", err)
		return
	}

	broadcastAddr := &net.UDPAddr{
		IP:   net.IPv4bcast,
		Port: DiscoveryPort,
	}

	if ld.conn4 != nil {
		_, err = ld.conn4.WriteToUDP(data, broadcastAddr)
		if err != nil {
			logging.Warn("Failed to send IPv4 broadcast: %v", err)
		}
	}

	if ld.conn6 != nil {
		multicastAddr := &net.UDPAddr{
			IP:   net.ParseIP("ff02::1"),
			Port: DiscoveryPort,
		}
		_, err = ld.conn6.WriteToUDP(data, multicastAddr)
		if err != nil {
			logging.Warn("Failed to send IPv6 multicast: %v", err)
		}
	}
}

func (ld *LocalDiscovery) receiveLoop() {
	defer ld.wg.Done()
	ourID, err := GetDeviceID(ld.store.GetDB())
	if err != nil {
		logging.Error(" Failed to get device ID: %v", err)
		return
	}

	buf := make([]byte, 8192)

	for {
		select {
		case <-ld.stopCh:
			return
		default:
		}

		ld.mu.Lock()
		running := ld.running
		ld.mu.Unlock()
		if !running {
			return
		}

		ld.readUDPConnection(ld.conn4, buf, ourID, "IPv4")
		ld.readUDPConnection(ld.conn6, buf, ourID, "IPv6")
	}
}

func (ld *LocalDiscovery) readUDPConnection(conn *net.UDPConn, buf []byte, ourID, ipVersion string) {
	if conn == nil {
		return
	}

	_ = conn.SetReadDeadline(time.Now().Add(100 * time.Millisecond))
	n, addr, err := conn.ReadFromUDP(buf)
	if err != nil {
		if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
			return
		}
		select {
		case <-ld.stopCh:
		default:
			logging.Warn("Failed to read %s UDP: %v", ipVersion, err)
		}
		return
	}

	ld.processPacket(buf[:n], addr.IP, ourID)
}

func (ld *LocalDiscovery) processPacket(data []byte, srcIP net.IP, ourID string) {
	packet, err := UnmarshalAnnouncement(data)
	if err != nil {
		return
	}

	if packet.DeviceID == ourID {
		return
	}

	if srcIP.IsLinkLocalUnicast() {
		return
	}

	peer := &Peer{
		ID:        packet.DeviceID,
		Name:      packet.Name,
		Address:   fmt.Sprintf("%s:%d", srcIP.String(), packet.Port),
		LastSeen:  time.Now().UnixNano(),
		Status:    "discovered",
		CreatedAt: time.Now().UnixNano(),
	}

	if err := ld.peerMgr.AddOrUpdatePeer(peer); err != nil {
		logging.Warn("Failed to add peer: %v", err)
	} else {
		logging.Info(" Discovered peer: %s (%s)", peer.Name, peer.Address)
	}
}

// Marshal serializes the announcement packet to bytes for UDP broadcast.
// Format: 4-byte magic | 36-byte device ID | 2-byte port | 1-byte name length | name.
// Returns error if device ID is not 36 chars or name exceeds 255 chars.
func (ap *AnnouncementPacket) Marshal() ([]byte, error) {
	buf := new(bytes.Buffer)

	if err := binary.Write(buf, binary.BigEndian, ap.Magic); err != nil {
		return nil, err
	}

	if len(ap.DeviceID) != 36 {
		return nil, fmt.Errorf("invalid device ID length: %d", len(ap.DeviceID))
	}
	buf.WriteString(ap.DeviceID)

	if err := binary.Write(buf, binary.BigEndian, ap.Port); err != nil {
		return nil, err
	}

	if len(ap.Name) > 255 {
		return nil, fmt.Errorf("name too long: %d", len(ap.Name))
	}
	buf.WriteByte(byte(len(ap.Name)))
	buf.WriteString(ap.Name)

	return buf.Bytes(), nil
}

// UnmarshalAnnouncement deserializes UDP broadcast data into an announcement packet.
// Validates magic number (0xD01744A7) and packet size (minimum 43 bytes).
// Returns error if packet is malformed or magic number doesn't match.
func UnmarshalAnnouncement(data []byte) (*AnnouncementPacket, error) {
	if len(data) < 43 {
		return nil, fmt.Errorf("packet too small: %d bytes", len(data))
	}

	buf := bytes.NewReader(data)

	var magic uint32
	if err := binary.Read(buf, binary.BigEndian, &magic); err != nil {
		return nil, err
	}

	if magic != MagicNumber {
		return nil, fmt.Errorf("invalid magic number: %x", magic)
	}

	deviceIDBytes := make([]byte, 36)
	if _, err := buf.Read(deviceIDBytes); err != nil {
		return nil, err
	}

	var port uint16
	if err := binary.Read(buf, binary.BigEndian, &port); err != nil {
		return nil, err
	}

	nameLen, err := buf.ReadByte()
	if err != nil {
		return nil, err
	}

	nameBytes := make([]byte, nameLen)
	if _, err := buf.Read(nameBytes); err != nil {
		return nil, err
	}

	return &AnnouncementPacket{
		Magic:    magic,
		DeviceID: string(deviceIDBytes),
		Port:     port,
		Name:     string(nameBytes),
	}, nil
}
