package sync

import (
	"bytes"
	"database/sql"
	"encoding/binary"
	"fmt"
	"log"
	"net"
	"sync"
	"time"
)

const (
	DiscoveryPort = 49151
	MagicNumber   = 0xD01744A7
)

type LocalDiscoveryStore interface {
	GetDB() *sql.DB
	AddOrUpdatePeer(peer *Peer) error
}

type LocalDiscovery struct {
	store   LocalDiscoveryStore
	port    int
	conn4   *net.UDPConn
	conn6   *net.UDPConn
	stopCh  chan struct{}
	mu      sync.Mutex
	running bool
}

type AnnouncementPacket struct {
	Magic    uint32
	DeviceID string
	Port     uint16
	Name     string
}

func NewLocalDiscovery(store LocalDiscoveryStore) *LocalDiscovery {
	return &LocalDiscovery{
		store:  store,
		stopCh: make(chan struct{}),
	}
}

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
		log.Printf("[WARN] Failed to set read buffer: %v", err)
	}

	ld.conn4 = conn4

	addr6 := &net.UDPAddr{
		IP:   net.IPv6zero,
		Port: DiscoveryPort,
	}
	conn6, err := net.ListenUDP("udp6", addr6)
	if err != nil {
		log.Printf("[WARN] Failed to bind IPv6 UDP: %v", err)
	} else {
		ld.conn6 = conn6
	}

	go ld.announceLoop()
	go ld.receiveLoop()

	log.Printf("[INFO] Local discovery started on port %d", DiscoveryPort)

	return nil
}

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
		ld.conn4.Close()
	}
	if ld.conn6 != nil {
		ld.conn6.Close()
	}

	return nil
}

func (ld *LocalDiscovery) announceLoop() {
	ticker := time.NewTicker(30 * time.Second)
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
		log.Printf("[ERROR] Failed to get device ID: %v", err)
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
		log.Printf("[ERROR] Failed to marshal packet: %v", err)
		return
	}

	broadcastAddr := &net.UDPAddr{
		IP:   net.IPv4bcast,
		Port: DiscoveryPort,
	}

	if ld.conn4 != nil {
		_, err = ld.conn4.WriteToUDP(data, broadcastAddr)
		if err != nil {
			log.Printf("Failed to send IPv4 broadcast: %v", err)
		}
	}

	if ld.conn6 != nil {
		multicastAddr := &net.UDPAddr{
			IP:   net.ParseIP("ff02::1"),
			Port: DiscoveryPort,
		}
		_, err = ld.conn6.WriteToUDP(data, multicastAddr)
		if err != nil {
			log.Printf("Failed to send IPv6 multicast: %v", err)
		}
	}
}

func (ld *LocalDiscovery) receiveLoop() {
	ourID, err := GetDeviceID(ld.store.GetDB())
	if err != nil {
		log.Printf("[ERROR] Failed to get device ID: %v", err)
		return
	}

	buf := make([]byte, 8192)

	for {
		select {
		case <-ld.stopCh:
			return
		default:
		}

		if ld.conn4 != nil {
			ld.conn4.SetReadDeadline(time.Now().Add(100 * time.Millisecond))
			n, addr, err := ld.conn4.ReadFromUDP(buf)
			if err != nil {
				if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
					continue
				}
				select {
				case <-ld.stopCh:
					return
				default:
					log.Printf("Failed to read IPv4 UDP: %v", err)
				}
				continue
			}

			ld.processPacket(buf[:n], addr.IP, ourID)
		}

		if ld.conn6 != nil {
			ld.conn6.SetReadDeadline(time.Now().Add(100 * time.Millisecond))
			n, addr, err := ld.conn6.ReadFromUDP(buf)
			if err != nil {
				if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
					continue
				}
				select {
				case <-ld.stopCh:
					return
				default:
				}
				continue
			}

			ld.processPacket(buf[:n], addr.IP, ourID)
		}
	}
}

func (ld *LocalDiscovery) processPacket(data []byte, srcIP net.IP, ourID string) {
	packet, err := UnmarshalAnnouncement(data)
	if err != nil {
		return
	}

	if packet.DeviceID == ourID {
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

	if err := ld.store.AddOrUpdatePeer(peer); err != nil {
		log.Printf("Failed to add peer: %v", err)
	} else {
		log.Printf("[INFO] Discovered peer: %s (%s)", peer.Name, peer.Address)
	}
}

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
