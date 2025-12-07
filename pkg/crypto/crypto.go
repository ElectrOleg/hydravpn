package crypto

import (
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"io"

	"golang.org/x/crypto/chacha20poly1305"
	"golang.org/x/crypto/curve25519"
	"golang.org/x/crypto/hkdf"
)

// KeyPair represents an X25519 key pair for key exchange
type KeyPair struct {
	PrivateKey [32]byte
	PublicKey  [32]byte
}

// Session holds the encryption state for a VPN session
type Session struct {
	SendKey    [32]byte
	RecvKey    [32]byte
	SendNonce  uint64
	RecvNonce  uint64
	sendCipher cipher.AEAD
	recvCipher cipher.AEAD
}

// GenerateKeyPair generates a new X25519 key pair
func GenerateKeyPair() (*KeyPair, error) {
	kp := &KeyPair{}

	// Generate random private key
	if _, err := io.ReadFull(rand.Reader, kp.PrivateKey[:]); err != nil {
		return nil, err
	}

	// Clamp private key for X25519
	kp.PrivateKey[0] &= 248
	kp.PrivateKey[31] &= 127
	kp.PrivateKey[31] |= 64

	// Derive public key
	curve25519.ScalarBaseMult(&kp.PublicKey, &kp.PrivateKey)

	return kp, nil
}

// ComputeSharedSecret computes the shared secret using X25519
func ComputeSharedSecret(privateKey, peerPublicKey [32]byte) ([32]byte, error) {
	var sharedSecret [32]byte
	curve25519.ScalarMult(&sharedSecret, &privateKey, &peerPublicKey)

	// Check for low-order points
	var zero [32]byte
	if sharedSecret == zero {
		return zero, errors.New("invalid shared secret: low-order point")
	}

	return sharedSecret, nil
}

// DeriveSessionKeys derives send and receive keys from shared secret using HKDF
func DeriveSessionKeys(sharedSecret [32]byte, isInitiator bool, salt []byte) (*Session, error) {
	// Use HKDF to derive keys
	hkdfReader := hkdf.New(sha256.New, sharedSecret[:], salt, []byte("hydravpn-session-keys"))

	var key1, key2 [32]byte
	if _, err := io.ReadFull(hkdfReader, key1[:]); err != nil {
		return nil, err
	}
	if _, err := io.ReadFull(hkdfReader, key2[:]); err != nil {
		return nil, err
	}

	session := &Session{}

	// Initiator sends with key1, receives with key2
	// Responder sends with key2, receives with key1
	if isInitiator {
		session.SendKey = key1
		session.RecvKey = key2
	} else {
		session.SendKey = key2
		session.RecvKey = key1
	}

	// Initialize ciphers using XChaCha20-Poly1305
	var err error
	session.sendCipher, err = chacha20poly1305.NewX(session.SendKey[:])
	if err != nil {
		return nil, err
	}

	session.recvCipher, err = chacha20poly1305.NewX(session.RecvKey[:])
	if err != nil {
		return nil, err
	}

	return session, nil
}

// Encrypt encrypts plaintext using XChaCha20-Poly1305
func (s *Session) Encrypt(plaintext []byte) ([]byte, error) {
	// Create nonce from counter
	nonce := make([]byte, 24)
	binary.LittleEndian.PutUint64(nonce, s.SendNonce)
	s.SendNonce++

	// Add random bytes to nonce for extra security
	if _, err := io.ReadFull(rand.Reader, nonce[8:]); err != nil {
		return nil, err
	}

	// Encrypt with AEAD
	ciphertext := s.sendCipher.Seal(nonce, nonce, plaintext, nil)

	return ciphertext, nil
}

// Decrypt decrypts ciphertext using XChaCha20-Poly1305
func (s *Session) Decrypt(ciphertext []byte) ([]byte, error) {
	if len(ciphertext) < 24 {
		return nil, errors.New("ciphertext too short")
	}

	// Extract nonce
	nonce := ciphertext[:24]
	ciphertext = ciphertext[24:]

	// Decrypt
	plaintext, err := s.recvCipher.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return nil, err
	}

	s.RecvNonce++
	return plaintext, nil
}

// GenerateRandomBytes generates cryptographically secure random bytes
func GenerateRandomBytes(n int) ([]byte, error) {
	b := make([]byte, n)
	if _, err := io.ReadFull(rand.Reader, b); err != nil {
		return nil, err
	}
	return b, nil
}

