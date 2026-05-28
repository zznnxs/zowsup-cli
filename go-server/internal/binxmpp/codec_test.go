package binxmpp

import (
	"bytes"
	"encoding/hex"
	"reflect"
	"testing"
)

func TestRoundTripEmptyTag(t *testing.T) {
	n := &Node{Tag: "iq"}
	encoded, err := Encode(n, false)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	got, err := Decode(encoded)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.Tag != "iq" {
		t.Fatalf("tag mismatch: got %q", got.Tag)
	}
	if got.HasChildren() || len(got.Data) > 0 {
		t.Fatalf("expected leaf node, got %+v", got)
	}
}

func TestRoundTripWithAttrs(t *testing.T) {
	n := &Node{
		Tag: "iq",
		Attrs: map[string]string{
			"type": "get",
			"id":   "abc",
			"to":   "s.whatsapp.net",
		},
	}
	encoded, err := Encode(n, false)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	got, err := Decode(encoded)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.Tag != "iq" || !reflect.DeepEqual(got.Attrs, n.Attrs) {
		t.Fatalf("mismatch: got %+v", got)
	}
}

func TestRoundTripChildren(t *testing.T) {
	n := &Node{
		Tag: "iq",
		Attrs: map[string]string{
			"type": "get",
			"id":   "1",
			"to":   "s.whatsapp.net",
		},
		Children: []*Node{{Tag: "ping"}},
	}
	encoded, err := Encode(n, false)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	got, err := Decode(encoded)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(got.Children) != 1 || got.Children[0].Tag != "ping" {
		t.Fatalf("missing/wrong child: %+v", got.Children)
	}
}

func TestRoundTripBinaryData(t *testing.T) {
	data := []byte{0x01, 0x02, 0x03, 0xff}
	n := &Node{
		Tag:   "enc",
		Attrs: map[string]string{"v": "2", "type": "msg"},
		Data:  data,
	}
	encoded, err := Encode(n, false)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	got, err := Decode(encoded)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !bytes.Equal(got.Data, data) {
		t.Fatalf("data mismatch: %x", got.Data)
	}
}

func TestRoundTripJID(t *testing.T) {
	// JIDs with an explicit agent prefix (".N:") round-trip to themselves;
	// JIDs that omit the agent (e.g. "628111:0@...") are reconstructed on
	// decode with agent=0 ("628111.0:0@..."), which matches the reference
	// Python behaviour — the wire format always carries the agent byte.
	cases := []struct{ in, want string }{
		{"628111@s.whatsapp.net", "628111@s.whatsapp.net"},
		{"628111.0:0@s.whatsapp.net", "628111.0:0@s.whatsapp.net"},
		{"628111.0:5@s.whatsapp.net", "628111.0:5@s.whatsapp.net"},
		{"628111:0@s.whatsapp.net", "628111.0:0@s.whatsapp.net"},
		{"628111:5@s.whatsapp.net", "628111.0:5@s.whatsapp.net"},
		{"123abc@lid", "123abc@lid"},
	}
	for _, c := range cases {
		t.Run(c.in, func(t *testing.T) {
			n := &Node{Tag: "to", Attrs: map[string]string{"jid": c.in}}
			encoded, err := Encode(n, false)
			if err != nil {
				t.Fatalf("encode: %v", err)
			}
			got, err := Decode(encoded)
			if err != nil {
				t.Fatalf("decode: %v", err)
			}
			if got.Attrs["jid"] != c.want {
				t.Fatalf("jid mismatch: got %q want %q", got.Attrs["jid"], c.want)
			}
		})
	}
}

func TestRoundTripDeflate(t *testing.T) {
	n := &Node{Tag: "iq", Attrs: map[string]string{"type": "get"}}
	encoded, err := Encode(n, true)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	if encoded[0]&FlagDeflate == 0 {
		t.Fatalf("expected deflate flag, got %02x", encoded[0])
	}
	got, err := Decode(encoded)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.Tag != "iq" || got.Attrs["type"] != "get" {
		t.Fatalf("mismatch after deflate roundtrip: %+v", got)
	}
}

// decodePythonHex verifies that we can decode wire output emitted by the
// reference Python encoder. The hex strings below were captured from
// core/layers/coder/encoder.py via scripts/regen_token_tests.py
// (see TestPythonGoldenVectors for the catalogue).
func decodePythonHex(t *testing.T, h string) *Node {
	t.Helper()
	raw, err := hex.DecodeString(h)
	if err != nil {
		t.Fatalf("bad hex: %v", err)
	}
	got, err := Decode(raw)
	if err != nil {
		t.Fatalf("decode python hex %q: %v", h, err)
	}
	return got
}

func TestPythonGoldenVectors(t *testing.T) {
	t.Run("empty_iq", func(t *testing.T) {
		got := decodePythonHex(t, "00f80119")
		if got.Tag != "iq" || got.HasChildren() || len(got.Data) > 0 {
			t.Fatalf("unexpected: %+v", got)
		}
	})

	t.Run("iq_with_attrs", func(t *testing.T) {
		got := decodePythonHex(t, "00f80719042908fc036162631103")
		if got.Tag != "iq" {
			t.Fatalf("tag: %s", got.Tag)
		}
		want := map[string]string{
			"type": "get",
			"id":   "abc",
			"to":   "s.whatsapp.net",
		}
		if !reflect.DeepEqual(got.Attrs, want) {
			t.Fatalf("attrs mismatch: got %v want %v", got.Attrs, want)
		}
	})

	t.Run("iq_ping_children", func(t *testing.T) {
		got := decodePythonHex(t, "00f80819042908551103f801f80156")
		if got.Tag != "iq" || len(got.Children) != 1 || got.Children[0].Tag != "ping" {
			t.Fatalf("unexpected: %+v", got)
		}
	})

	t.Run("enc_data", func(t *testing.T) {
		got := decodePythonHex(t, "00f8061d5145044dfc04010203ff")
		if got.Tag != "enc" {
			t.Fatalf("tag: %s", got.Tag)
		}
		if !bytes.Equal(got.Data, []byte{0x01, 0x02, 0x03, 0xff}) {
			t.Fatalf("data: %x", got.Data)
		}
	})

	t.Run("msg_with_packed_jid", func(t *testing.T) {
		got := decodePythonHex(t, "00f8071311faff036281110308fc026d310438")
		if got.Tag != "message" {
			t.Fatalf("tag: %s", got.Tag)
		}
		if jid := got.Attrs["to"]; jid != "628111@s.whatsapp.net" {
			t.Fatalf("jid: %s", jid)
		}
		if got.Attrs["id"] != "m1" || got.Attrs["type"] != "text" {
			t.Fatalf("other attrs: %v", got.Attrs)
		}
	})
}

func TestPackedNibbleDigits(t *testing.T) {
	// pure-digit strings should pack via the 255 alphabet.
	n := &Node{Tag: "x", Attrs: map[string]string{"id": "0123456789"}}
	encoded, err := Encode(n, false)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	got, err := Decode(encoded)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.Attrs["id"] != "0123456789" {
		t.Fatalf("digit packing mismatch: %s", got.Attrs["id"])
	}
}

func TestPackedHexUpper(t *testing.T) {
	n := &Node{Tag: "x", Attrs: map[string]string{"sha": "ABCDEF0123"}}
	encoded, err := Encode(n, false)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	got, err := Decode(encoded)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.Attrs["sha"] != "ABCDEF0123" {
		t.Fatalf("hex packing mismatch: %s", got.Attrs["sha"])
	}
}

func TestLargeData(t *testing.T) {
	big := make([]byte, 0x12345)
	for i := range big {
		big[i] = byte(i)
	}
	n := &Node{Tag: "blob", Data: big}
	encoded, err := Encode(n, false)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	got, err := Decode(encoded)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !bytes.Equal(got.Data, big) {
		t.Fatalf("large data mismatch (len %d vs %d)", len(got.Data), len(big))
	}
}
