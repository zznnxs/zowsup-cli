// Package noise implements the Noise XX handshake variant used by
// WhatsApp's "Multi-Device" client protocol.
//
// References:
//   - Noise spec, revision 34 (https://noiseprotocol.org/noise.html)
//   - consonance/async_handshake.py (in this repo) — the Python implementation
//     this Go port mirrors.
//
// WhatsApp differs from stock Noise_XX in exactly one place: SymmetricState.
// In stock Noise, EncryptAndHash always feeds the ciphertext into MixHash;
// WhatsApp only does so when the cipher already has a key. That single
// behavioural difference is captured by [SymmetricState.WAVariant].
//
// Caveats for M0: this milestone ships the core state machine (X25519 /
// AES-GCM-256 / SHA-256 / XX initiator role) plus unit tests for an
// initiator-responder loopback. It does not yet wire up the IK pattern or
// the XX-fallback path used when the server rejects our cached static key
// — those are part of M1 / M5.
package noise

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"

	"golang.org/x/crypto/curve25519"
)

// DHKeyLen is the length (in bytes) of an X25519 public/private key.
const DHKeyLen = 32

// HashLen is the SHA-256 digest length.
const HashLen = 32

// MacLen is the AES-GCM tag length.
const MacLen = 16

// KeyPair holds an X25519 keypair. Generate new ones with GenerateKey.
type KeyPair struct {
	Public  [DHKeyLen]byte
	Private [DHKeyLen]byte
}

// GenerateKey draws a fresh X25519 keypair from crypto/rand.
func GenerateKey() (*KeyPair, error) {
	var kp KeyPair
	if _, err := rand.Read(kp.Private[:]); err != nil {
		return nil, fmt.Errorf("noise: read random: %w", err)
	}
	// curve25519 clamping is applied by curve25519.X25519 internally.
	pub, err := curve25519.X25519(kp.Private[:], curve25519.Basepoint)
	if err != nil {
		return nil, fmt.Errorf("noise: derive public: %w", err)
	}
	copy(kp.Public[:], pub)
	return &kp, nil
}

// KeyPairFromPrivate constructs a KeyPair from an existing private scalar.
func KeyPairFromPrivate(priv [DHKeyLen]byte) (*KeyPair, error) {
	kp := KeyPair{Private: priv}
	pub, err := curve25519.X25519(priv[:], curve25519.Basepoint)
	if err != nil {
		return nil, fmt.Errorf("noise: derive public: %w", err)
	}
	copy(kp.Public[:], pub)
	return &kp, nil
}

// dh returns priv * pub on Curve25519.
func dh(priv, pub [DHKeyLen]byte) ([DHKeyLen]byte, error) {
	var out [DHKeyLen]byte
	shared, err := curve25519.X25519(priv[:], pub[:])
	if err != nil {
		return out, err
	}
	copy(out[:], shared)
	return out, nil
}

// ---------------------------------------------------------------------------
// CipherState
// ---------------------------------------------------------------------------

// CipherState wraps an AES-GCM-256 key plus a 64-bit nonce counter, per the
// Noise spec.
type CipherState struct {
	key   [32]byte
	nonce uint64
	has   bool
}

// HasKey reports whether the state currently holds an encryption key.
func (c *CipherState) HasKey() bool { return c.has }

// initializeKey resets the state with key k and counter 0.
func (c *CipherState) initializeKey(k [32]byte) {
	c.key = k
	c.nonce = 0
	c.has = true
}

// encryptWithAd encrypts plaintext with the current key and ad and advances
// the nonce. If no key is set the plaintext is returned unchanged.
func (c *CipherState) encryptWithAd(ad, plaintext []byte) ([]byte, error) {
	if !c.has {
		return append([]byte(nil), plaintext...), nil
	}
	gcm, err := newGCM(c.key)
	if err != nil {
		return nil, err
	}
	nonce := gcmNonce(c.nonce)
	ct := gcm.Seal(nil, nonce[:], plaintext, ad)
	c.nonce++
	return ct, nil
}

// decryptWithAd is the inverse of encryptWithAd.
func (c *CipherState) decryptWithAd(ad, ciphertext []byte) ([]byte, error) {
	if !c.has {
		return append([]byte(nil), ciphertext...), nil
	}
	gcm, err := newGCM(c.key)
	if err != nil {
		return nil, err
	}
	nonce := gcmNonce(c.nonce)
	pt, err := gcm.Open(nil, nonce[:], ciphertext, ad)
	if err != nil {
		return nil, fmt.Errorf("noise: decrypt: %w", err)
	}
	c.nonce++
	return pt, nil
}

// Encrypt is the post-handshake helper used by the transport cipher pair to
// encrypt application data. Equivalent to EncryptWithAd(nil, plaintext).
func (c *CipherState) Encrypt(ad, plaintext []byte) ([]byte, error) {
	return c.encryptWithAd(ad, plaintext)
}

// Decrypt is the post-handshake helper used by the transport cipher pair.
func (c *CipherState) Decrypt(ad, ciphertext []byte) ([]byte, error) {
	return c.decryptWithAd(ad, ciphertext)
}

func newGCM(key [32]byte) (cipher.AEAD, error) {
	block, err := aes.NewCipher(key[:])
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}

// gcmNonce builds the 12-byte AES-GCM nonce from a Noise 64-bit counter.
// Per the spec the first 4 bytes are zero (so the counter is stored
// big-endian in bytes [4:12]).
func gcmNonce(n uint64) [12]byte {
	var out [12]byte
	binary.BigEndian.PutUint64(out[4:], n)
	return out
}

// ---------------------------------------------------------------------------
// SymmetricState
// ---------------------------------------------------------------------------

// SymmetricState carries the chaining key and hash for the handshake. When
// WAVariant is true, EncryptAndHash skips the MixHash on the produced
// ciphertext if the cipher does not yet have a key — matching WhatsApp's
// custom WASymmetricState.
type SymmetricState struct {
	WAVariant bool

	cipher CipherState
	ck     [HashLen]byte
	h      [HashLen]byte
}

// initializeSymmetric sets h to either the protocol name (if ≤32 bytes) or
// SHA256(protocolName), and ck := h. The cipher key is cleared.
func (s *SymmetricState) initializeSymmetric(protocolName []byte) {
	if len(protocolName) <= HashLen {
		var buf [HashLen]byte
		copy(buf[:], protocolName)
		s.h = buf
	} else {
		s.h = sha256.Sum256(protocolName)
	}
	s.ck = s.h
	s.cipher = CipherState{}
}

// mixKey runs HKDF(ck, inputKeyMaterial, 2) → ck, k.
func (s *SymmetricState) mixKey(input []byte) {
	out := hkdf(s.ck[:], input, 2)
	copy(s.ck[:], out[0])
	var k [32]byte
	copy(k[:], out[1])
	s.cipher.initializeKey(k)
}

// mixHash updates h := SHA256(h || data).
func (s *SymmetricState) mixHash(data []byte) {
	hasher := sha256.New()
	hasher.Write(s.h[:])
	hasher.Write(data)
	hasher.Sum(s.h[:0])
}

// encryptAndHash encrypts plaintext with the current cipher and AD=h, then
// MixHash(ciphertext). The WAVariant flag suppresses MixHash when there is
// no key (matching WhatsApp's custom implementation).
func (s *SymmetricState) encryptAndHash(plaintext []byte) ([]byte, error) {
	ct, err := s.cipher.encryptWithAd(s.h[:], plaintext)
	if err != nil {
		return nil, err
	}
	if s.WAVariant {
		if s.cipher.HasKey() {
			s.mixHash(ct)
		}
	} else {
		s.mixHash(ct)
	}
	return ct, nil
}

// decryptAndHash mirrors encryptAndHash on the receive side.
func (s *SymmetricState) decryptAndHash(ciphertext []byte) ([]byte, error) {
	pt, err := s.cipher.decryptWithAd(s.h[:], ciphertext)
	if err != nil {
		return nil, err
	}
	if s.WAVariant {
		if s.cipher.HasKey() {
			s.mixHash(ciphertext)
		}
	} else {
		s.mixHash(ciphertext)
	}
	return pt, nil
}

// split derives the post-handshake send/recv CipherStates.
func (s *SymmetricState) split() (send, recv CipherState) {
	out := hkdf(s.ck[:], nil, 2)
	var k1, k2 [32]byte
	copy(k1[:], out[0])
	copy(k2[:], out[1])
	send.initializeKey(k1)
	recv.initializeKey(k2)
	return
}

// hkdf is the Noise HKDF variant: returns `outputs` 32-byte segments
// derived from chainingKey || inputKeyMaterial.
func hkdf(chainingKey, ikm []byte, outputs int) [][]byte {
	tempKey := hmacSHA256(chainingKey, ikm)
	out := make([][]byte, 0, outputs)
	var prev []byte
	for i := 1; i <= outputs; i++ {
		input := append(append([]byte(nil), prev...), byte(i))
		next := hmacSHA256(tempKey, input)
		out = append(out, next)
		prev = next
	}
	return out
}

func hmacSHA256(key, msg []byte) []byte {
	m := hmac.New(sha256.New, key)
	m.Write(msg)
	return m.Sum(nil)
}

// ---------------------------------------------------------------------------
// HandshakeState — Noise_XX initiator
// ---------------------------------------------------------------------------

// HandshakeState implements the initiator side of the Noise_XX pattern with
// WhatsApp's prologue.
//
// XX message tokens:
//
//	-> e
//	<- e, ee, s, es
//	-> s, se
type HandshakeState struct {
	sym       SymmetricState
	s         KeyPair    // local static
	e         KeyPair    // local ephemeral
	rs        [32]byte   // remote static (filled by msg 2)
	re        [32]byte   // remote ephemeral
	hasRS     bool
	hasRE     bool
	initiator bool
}

const xxProtocolName = "Noise_XX_25519_AESGCM_SHA256"

// NewXXInitiator builds an XX initiator state, ready for WriteMessage1.
// prologue is the bytes mixed in before any DH (typically `WA\x06\x03`).
// staticKey is the long-lived noise static used by this client.
// waVariant should be true when speaking to a real WhatsApp server.
func NewXXInitiator(prologue []byte, staticKey *KeyPair, waVariant bool) (*HandshakeState, error) {
	if staticKey == nil {
		return nil, errors.New("noise: static key required")
	}
	hs := &HandshakeState{
		initiator: true,
		s:         *staticKey,
		sym:       SymmetricState{WAVariant: waVariant},
	}
	hs.sym.initializeSymmetric([]byte(xxProtocolName))
	hs.sym.mixHash(prologue)
	return hs, nil
}

// NewXXResponder mirrors NewXXInitiator for the responder side; used by tests.
func NewXXResponder(prologue []byte, staticKey *KeyPair, waVariant bool) (*HandshakeState, error) {
	if staticKey == nil {
		return nil, errors.New("noise: static key required")
	}
	hs := &HandshakeState{
		initiator: false,
		s:         *staticKey,
		sym:       SymmetricState{WAVariant: waVariant},
	}
	hs.sym.initializeSymmetric([]byte(xxProtocolName))
	hs.sym.mixHash(prologue)
	return hs, nil
}

// LocalStatic returns the local static public key, e.g. for cert validation.
func (hs *HandshakeState) LocalStatic() [DHKeyLen]byte { return hs.s.Public }

// RemoteStatic returns the remote static once known (after read of msg 2 on
// initiator, or after read of msg 3 on responder). Reports !ok before then.
func (hs *HandshakeState) RemoteStatic() (rs [DHKeyLen]byte, ok bool) {
	if !hs.hasRS {
		return rs, false
	}
	return hs.rs, true
}

// HandshakeHash returns the post-handshake handshake hash, useful for
// channel-binding (e.g. cert validation).
func (hs *HandshakeState) HandshakeHash() [HashLen]byte { return hs.sym.h }

// WriteMessage1 emits the initiator's first message:
//
//	-> e, encrypted(payload)
//
// e is freshly generated unless eOverride is non-nil (used by tests + IK).
func (hs *HandshakeState) WriteMessage1(payload []byte, eOverride *KeyPair) ([]byte, error) {
	if !hs.initiator {
		return nil, errors.New("noise: responder cannot WriteMessage1")
	}
	if eOverride != nil {
		hs.e = *eOverride
	} else {
		kp, err := GenerateKey()
		if err != nil {
			return nil, err
		}
		hs.e = *kp
	}
	hs.sym.mixHash(hs.e.Public[:])
	out := make([]byte, 0, DHKeyLen+len(payload)+MacLen)
	out = append(out, hs.e.Public[:]...)
	ct, err := hs.sym.encryptAndHash(payload)
	if err != nil {
		return nil, err
	}
	out = append(out, ct...)
	return out, nil
}

// ReadMessage1 (responder) parses the initiator's first message and returns
// the decrypted payload.
func (hs *HandshakeState) ReadMessage1(msg []byte) ([]byte, error) {
	if hs.initiator {
		return nil, errors.New("noise: initiator cannot ReadMessage1")
	}
	if len(msg) < DHKeyLen {
		return nil, errors.New("noise: msg1 too short")
	}
	copy(hs.re[:], msg[:DHKeyLen])
	hs.hasRE = true
	hs.sym.mixHash(hs.re[:])
	return hs.sym.decryptAndHash(msg[DHKeyLen:])
}

// WriteMessage2 (responder) emits:
//
//	<- e, ee, s, es, encrypted(payload)
//
// payload typically carries the remote static cert / signed payload.
func (hs *HandshakeState) WriteMessage2(payload []byte, eOverride *KeyPair) ([]byte, error) {
	if hs.initiator {
		return nil, errors.New("noise: initiator cannot WriteMessage2")
	}
	if !hs.hasRE {
		return nil, errors.New("noise: WriteMessage2 before ReadMessage1")
	}
	if eOverride != nil {
		hs.e = *eOverride
	} else {
		kp, err := GenerateKey()
		if err != nil {
			return nil, err
		}
		hs.e = *kp
	}
	hs.sym.mixHash(hs.e.Public[:])
	out := make([]byte, 0, 2*DHKeyLen+DHKeyLen+MacLen+len(payload)+MacLen)
	out = append(out, hs.e.Public[:]...)
	// ee
	shared, err := dh(hs.e.Private, hs.re)
	if err != nil {
		return nil, err
	}
	hs.sym.mixKey(shared[:])
	// s (encrypted)
	encS, err := hs.sym.encryptAndHash(hs.s.Public[:])
	if err != nil {
		return nil, err
	}
	out = append(out, encS...)
	// es
	shared2, err := dh(hs.s.Private, hs.re)
	if err != nil {
		return nil, err
	}
	hs.sym.mixKey(shared2[:])
	// payload (encrypted)
	encPL, err := hs.sym.encryptAndHash(payload)
	if err != nil {
		return nil, err
	}
	out = append(out, encPL...)
	return out, nil
}

// ReadMessage2 (initiator) processes the server's reply and returns the
// decrypted payload. After return, RemoteStatic() yields the server's rs.
func (hs *HandshakeState) ReadMessage2(msg []byte) ([]byte, error) {
	if !hs.initiator {
		return nil, errors.New("noise: responder cannot ReadMessage2")
	}
	if len(msg) < 2*DHKeyLen+MacLen+MacLen {
		return nil, errors.New("noise: msg2 too short")
	}
	copy(hs.re[:], msg[:DHKeyLen])
	hs.hasRE = true
	hs.sym.mixHash(hs.re[:])
	// ee
	shared, err := dh(hs.e.Private, hs.re)
	if err != nil {
		return nil, err
	}
	hs.sym.mixKey(shared[:])
	// s
	encS := msg[DHKeyLen : 2*DHKeyLen+MacLen]
	rsBytes, err := hs.sym.decryptAndHash(encS)
	if err != nil {
		return nil, fmt.Errorf("noise: decrypt rs: %w", err)
	}
	if len(rsBytes) != DHKeyLen {
		return nil, errors.New("noise: bad rs length")
	}
	copy(hs.rs[:], rsBytes)
	hs.hasRS = true
	// es
	shared2, err := dh(hs.e.Private, hs.rs)
	if err != nil {
		return nil, err
	}
	hs.sym.mixKey(shared2[:])
	// payload
	encPL := msg[2*DHKeyLen+MacLen:]
	return hs.sym.decryptAndHash(encPL)
}

// WriteMessage3 (initiator) emits:
//
//	-> s, se, encrypted(payload)
//
// and returns the on-wire bytes plus the post-handshake (send, recv)
// CipherStates. After this call the handshake is complete.
func (hs *HandshakeState) WriteMessage3(payload []byte) ([]byte, CipherState, CipherState, error) {
	if !hs.initiator {
		return nil, CipherState{}, CipherState{}, errors.New("noise: responder cannot WriteMessage3")
	}
	// s (encrypted)
	encS, err := hs.sym.encryptAndHash(hs.s.Public[:])
	if err != nil {
		return nil, CipherState{}, CipherState{}, err
	}
	// se
	shared, err := dh(hs.s.Private, hs.re)
	if err != nil {
		return nil, CipherState{}, CipherState{}, err
	}
	hs.sym.mixKey(shared[:])
	encPL, err := hs.sym.encryptAndHash(payload)
	if err != nil {
		return nil, CipherState{}, CipherState{}, err
	}
	out := make([]byte, 0, len(encS)+len(encPL))
	out = append(out, encS...)
	out = append(out, encPL...)
	send, recv := hs.sym.split()
	return out, send, recv, nil
}

// ReadMessage3 (responder) processes the initiator's final message and
// returns (decryptedPayload, send, recv).
func (hs *HandshakeState) ReadMessage3(msg []byte) ([]byte, CipherState, CipherState, error) {
	if hs.initiator {
		return nil, CipherState{}, CipherState{}, errors.New("noise: initiator cannot ReadMessage3")
	}
	if len(msg) < DHKeyLen+MacLen+MacLen {
		return nil, CipherState{}, CipherState{}, errors.New("noise: msg3 too short")
	}
	encS := msg[:DHKeyLen+MacLen]
	rsBytes, err := hs.sym.decryptAndHash(encS)
	if err != nil {
		return nil, CipherState{}, CipherState{}, fmt.Errorf("noise: decrypt rs: %w", err)
	}
	if len(rsBytes) != DHKeyLen {
		return nil, CipherState{}, CipherState{}, errors.New("noise: bad rs length")
	}
	copy(hs.rs[:], rsBytes)
	hs.hasRS = true
	shared, err := dh(hs.e.Private, hs.rs)
	if err != nil {
		return nil, CipherState{}, CipherState{}, err
	}
	hs.sym.mixKey(shared[:])
	encPL := msg[DHKeyLen+MacLen:]
	pl, err := hs.sym.decryptAndHash(encPL)
	if err != nil {
		return nil, CipherState{}, CipherState{}, err
	}
	// Responder's send/recv are reversed relative to the initiator.
	recv, send := hs.sym.split()
	return pl, send, recv, nil
}
