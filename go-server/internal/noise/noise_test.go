package noise

import (
	"bytes"
	"testing"
)

// TestXXLoopback runs a full XX initiator/responder handshake in-process and
// then exchanges one application data message in each direction.
func TestXXLoopback(t *testing.T) {
	prologue := []byte("WA\x06\x03")
	initStatic, err := GenerateKey()
	if err != nil {
		t.Fatal(err)
	}
	respStatic, err := GenerateKey()
	if err != nil {
		t.Fatal(err)
	}
	ini, err := NewXXInitiator(prologue, initStatic, true)
	if err != nil {
		t.Fatal(err)
	}
	res, err := NewXXResponder(prologue, respStatic, true)
	if err != nil {
		t.Fatal(err)
	}

	msg1, err := ini.WriteMessage1([]byte("hello-from-init"), nil)
	if err != nil {
		t.Fatalf("WriteMessage1: %v", err)
	}
	got1, err := res.ReadMessage1(msg1)
	if err != nil {
		t.Fatalf("ReadMessage1: %v", err)
	}
	if !bytes.Equal(got1, []byte("hello-from-init")) {
		t.Fatalf("msg1 payload mismatch: %q", got1)
	}

	msg2, err := res.WriteMessage2([]byte("hello-from-resp"), nil)
	if err != nil {
		t.Fatalf("WriteMessage2: %v", err)
	}
	got2, err := ini.ReadMessage2(msg2)
	if err != nil {
		t.Fatalf("ReadMessage2: %v", err)
	}
	if !bytes.Equal(got2, []byte("hello-from-resp")) {
		t.Fatalf("msg2 payload mismatch: %q", got2)
	}
	rs, ok := ini.RemoteStatic()
	if !ok || rs != respStatic.Public {
		t.Fatalf("initiator did not learn responder static; got %x ok=%v", rs, ok)
	}

	msg3, iniSend, iniRecv, err := ini.WriteMessage3([]byte("client-finish"))
	if err != nil {
		t.Fatalf("WriteMessage3: %v", err)
	}
	got3, resSend, resRecv, err := res.ReadMessage3(msg3)
	if err != nil {
		t.Fatalf("ReadMessage3: %v", err)
	}
	if !bytes.Equal(got3, []byte("client-finish")) {
		t.Fatalf("msg3 payload mismatch: %q", got3)
	}
	rsFromResp, ok := res.RemoteStatic()
	if !ok || rsFromResp != initStatic.Public {
		t.Fatalf("responder did not learn initiator static")
	}

	// Check both sides derived a matching handshake hash.
	if ini.HandshakeHash() != res.HandshakeHash() {
		t.Fatalf("handshake hash mismatch")
	}

	// Exchange application data both ways.
	ct, err := iniSend.Encrypt(nil, []byte("ping"))
	if err != nil {
		t.Fatalf("init.Encrypt: %v", err)
	}
	pt, err := resRecv.Decrypt(nil, ct)
	if err != nil {
		t.Fatalf("resp.Decrypt: %v", err)
	}
	if !bytes.Equal(pt, []byte("ping")) {
		t.Fatalf("ping mismatch: %q", pt)
	}

	ct2, err := resSend.Encrypt(nil, []byte("pong"))
	if err != nil {
		t.Fatalf("resp.Encrypt: %v", err)
	}
	pt2, err := iniRecv.Decrypt(nil, ct2)
	if err != nil {
		t.Fatalf("init.Decrypt: %v", err)
	}
	if !bytes.Equal(pt2, []byte("pong")) {
		t.Fatalf("pong mismatch: %q", pt2)
	}
}

func TestSymmetricStateInitialize(t *testing.T) {
	cases := []struct {
		name string
		in   []byte
	}{
		{"short_name", []byte("Noise_XX_25519_AESGCM_SHA256")},
		{"long_name", bytes.Repeat([]byte{'x'}, 100)},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			s := &SymmetricState{}
			s.initializeSymmetric(c.in)
			if s.h == ([HashLen]byte{}) {
				t.Fatal("h not initialized")
			}
			if s.ck != s.h {
				t.Fatal("ck must equal h after init")
			}
			if s.cipher.HasKey() {
				t.Fatal("cipher must not have key after init")
			}
		})
	}
}

func TestCipherStateNonceIncrement(t *testing.T) {
	var key [32]byte
	for i := range key {
		key[i] = byte(i)
	}
	cs := CipherState{}
	cs.initializeKey(key)
	ct1, err := cs.Encrypt(nil, []byte("a"))
	if err != nil {
		t.Fatal(err)
	}
	ct2, err := cs.Encrypt(nil, []byte("a"))
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(ct1, ct2) {
		t.Fatal("nonce must increment between calls")
	}
}

func TestWAVariantSkipsInitialMixHash(t *testing.T) {
	// With WAVariant=true, encryptAndHash on a stateless cipher must not
	// fold the (plaintext-returned-as-ciphertext) into the hash, but with
	// WAVariant=false it must. Confirm the two paths diverge.
	pl := []byte("payload")

	a := &SymmetricState{WAVariant: true}
	a.initializeSymmetric([]byte("p"))
	if _, err := a.encryptAndHash(pl); err != nil {
		t.Fatal(err)
	}

	b := &SymmetricState{WAVariant: false}
	b.initializeSymmetric([]byte("p"))
	if _, err := b.encryptAndHash(pl); err != nil {
		t.Fatal(err)
	}

	if a.h == b.h {
		t.Fatal("WA variant should diverge from stock Noise on first encrypt without key")
	}
}
