package protocol

import (
	"encoding/binary"
	"errors"
	"time"
)

// Protocol constants
const (
	// Magic bytes to identify HydraVPN packets
	MagicByte1 = 0x48 // 'H'
	MagicByte2 = 0x56 // 'V'
	
	// Protocol version
	ProtocolVersion = 1
	
	// Packet types
	PacketTypeHandshakeInit     = 0x01
	PacketTypeHandshakeResponse = 0x02
	PacketTypeData              = 0x03
	PacketTypeKeepAlive         = 0x04
	PacketTypeDisconnect        = 0x05
	
	// Maximum packet size
	MaxPacketSize = 65535
	
	// Header size: magic(2) + version(1) + type(1) + session_id(8) + length(2) = 14
	HeaderSize = 14
	
	// Handshake timeout
	HandshakeTimeout = 10 * time.Second
	
	// Keep-alive interval
	KeepAliveInterval = 25 * time.Second
)

// PacketHeader represents the header of a HydraVPN packet
type PacketHeader struct {
	Magic     [2]byte
	Version   uint8
	Type      uint8
	SessionID uint64
	Length    uint16
}

// Packet represents a complete HydraVPN packet
type Packet struct {
	Header  PacketHeader
	Payload []byte
}

// HandshakeInit is the first message from client to server
type HandshakeInit struct {
	ClientPublicKey [32]byte
	Timestamp       int64
	RandomPadding   [32]byte // Random padding to make packet size variable
}

// HandshakeResponse is the server's response to handshake init
type HandshakeResponse struct {
	ServerPublicKey [32]byte
	SessionID       uint64
	AssignedIP      [4]byte  // Client's assigned IP in the VPN
	ServerIP        [4]byte  // Server's IP in the VPN
	Subnet          uint8    // Subnet mask bits (e.g., 24 for /24)
	RandomPadding   [32]byte
}

// NewPacket creates a new packet with the given type and payload
func NewPacket(packetType uint8, sessionID uint64, payload []byte) *Packet {
	return &Packet{
		Header: PacketHeader{
			Magic:     [2]byte{MagicByte1, MagicByte2},
			Version:   ProtocolVersion,
			Type:      packetType,
			SessionID: sessionID,
			Length:    uint16(len(payload)),
		},
		Payload: payload,
	}
}

// Marshal serializes a packet to bytes
func (p *Packet) Marshal() []byte {
	buf := make([]byte, HeaderSize+len(p.Payload))
	
	// Write header
	buf[0] = p.Header.Magic[0]
	buf[1] = p.Header.Magic[1]
	buf[2] = p.Header.Version
	buf[3] = p.Header.Type
	binary.BigEndian.PutUint64(buf[4:12], p.Header.SessionID)
	binary.BigEndian.PutUint16(buf[12:14], p.Header.Length)
	
	// Write payload
	copy(buf[HeaderSize:], p.Payload)
	
	return buf
}

// UnmarshalPacket deserializes bytes to a packet
func UnmarshalPacket(data []byte) (*Packet, error) {
	if len(data) < HeaderSize {
		return nil, errors.New("packet too short")
	}
	
	p := &Packet{}
	
	// Read header
	p.Header.Magic[0] = data[0]
	p.Header.Magic[1] = data[1]
	
	// Validate magic bytes
	if p.Header.Magic[0] != MagicByte1 || p.Header.Magic[1] != MagicByte2 {
		return nil, errors.New("invalid magic bytes")
	}
	
	p.Header.Version = data[2]
	if p.Header.Version != ProtocolVersion {
		return nil, errors.New("unsupported protocol version")
	}
	
	p.Header.Type = data[3]
	p.Header.SessionID = binary.BigEndian.Uint64(data[4:12])
	p.Header.Length = binary.BigEndian.Uint16(data[12:14])
	
	// Validate length
	if int(p.Header.Length) != len(data)-HeaderSize {
		return nil, errors.New("payload length mismatch")
	}
	
	// Read payload
	p.Payload = make([]byte, p.Header.Length)
	copy(p.Payload, data[HeaderSize:])
	
	return p, nil
}

// MarshalHandshakeInit serializes handshake init message
func MarshalHandshakeInit(h *HandshakeInit) []byte {
	buf := make([]byte, 32+8+32) // public key + timestamp + padding
	copy(buf[0:32], h.ClientPublicKey[:])
	binary.BigEndian.PutUint64(buf[32:40], uint64(h.Timestamp))
	copy(buf[40:72], h.RandomPadding[:])
	return buf
}

// UnmarshalHandshakeInit deserializes handshake init message
func UnmarshalHandshakeInit(data []byte) (*HandshakeInit, error) {
	if len(data) < 72 {
		return nil, errors.New("handshake init too short")
	}
	
	h := &HandshakeInit{}
	copy(h.ClientPublicKey[:], data[0:32])
	h.Timestamp = int64(binary.BigEndian.Uint64(data[32:40]))
	copy(h.RandomPadding[:], data[40:72])
	
	return h, nil
}

// MarshalHandshakeResponse serializes handshake response message
func MarshalHandshakeResponse(h *HandshakeResponse) []byte {
	buf := make([]byte, 32+8+4+4+1+32) // pubkey + session + assigned_ip + server_ip + subnet + padding
	copy(buf[0:32], h.ServerPublicKey[:])
	binary.BigEndian.PutUint64(buf[32:40], h.SessionID)
	copy(buf[40:44], h.AssignedIP[:])
	copy(buf[44:48], h.ServerIP[:])
	buf[48] = h.Subnet
	copy(buf[49:81], h.RandomPadding[:])
	return buf
}

// UnmarshalHandshakeResponse deserializes handshake response message
func UnmarshalHandshakeResponse(data []byte) (*HandshakeResponse, error) {
	if len(data) < 81 {
		return nil, errors.New("handshake response too short")
	}
	
	h := &HandshakeResponse{}
	copy(h.ServerPublicKey[:], data[0:32])
	h.SessionID = binary.BigEndian.Uint64(data[32:40])
	copy(h.AssignedIP[:], data[40:44])
	copy(h.ServerIP[:], data[44:48])
	h.Subnet = data[48]
	copy(h.RandomPadding[:], data[49:81])
	
	return h, nil
}

// IsValidPacketType checks if packet type is valid
func IsValidPacketType(t uint8) bool {
	switch t {
	case PacketTypeHandshakeInit,
		PacketTypeHandshakeResponse,
		PacketTypeData,
		PacketTypeKeepAlive,
		PacketTypeDisconnect:
		return true
	}
	return false
}
