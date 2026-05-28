// Package binxmpp implements the WhatsApp binary XMPP node codec.
//
// Wire format (mirrors core/layers/coder in the Python project):
//
//   byte 0  — flag byte: bit 0 = SEGMENTED, bit 1 = DEFLATE.
//             when DEFLATE is set, bytes 1.. are zlib-compressed; after
//             decompression the leading flag byte is re-prepended.
//   byte 1+ — a single Node tree, encoded recursively.
//
// Each Node consists of:
//   1. a list-start token (0 / 248+u8 / 249+u16) giving the element count
//      n = 1 + 2*len(attrs) + hasData + hasChildren
//   2. a string for the tag (see writeString)
//   3. n attribute (key, value) string pairs
//   4. optionally a child list (0 / 248+u8 / 249+u16) followed by n child nodes,
//      OR an inline data payload (one of tokens 251/252/253/254/255 or a
//      directly written string token)
package binxmpp

import (
	"bytes"
	"compress/zlib"
	"errors"
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"
)

// sortStrings keeps attribute encoding deterministic. Extracted to a helper
// so the encoder body stays focused on protocol logic.
func sortStrings(s []string) { sort.Strings(s) }

// Flag bits for the leading byte of a stanza.
const (
	FlagSegmented byte = 0x01
	FlagDeflate   byte = 0x02
)

// Node is the parsed protocol tree node. Either Children or Data is set,
// never both; the wire format permits one or the other (or neither).
type Node struct {
	Tag      string
	Attrs    map[string]string
	Children []*Node
	Data     []byte
}

// HasChildren reports whether the node carries a child list. It is also
// used at encode time to choose between data and children branches.
func (n *Node) HasChildren() bool { return len(n.Children) > 0 }

// GetAttr returns the named attribute, or "" if not set.
func (n *Node) GetAttr(k string) string {
	if n == nil || n.Attrs == nil {
		return ""
	}
	return n.Attrs[k]
}

// FindChild returns the first child whose Tag equals tag, or nil.
func (n *Node) FindChild(tag string) *Node {
	if n == nil {
		return nil
	}
	for _, c := range n.Children {
		if c != nil && c.Tag == tag {
			return c
		}
	}
	return nil
}

// FindChildren returns all immediate children whose Tag equals tag.
func (n *Node) FindChildren(tag string) []*Node {
	if n == nil {
		return nil
	}
	var out []*Node
	for _, c := range n.Children {
		if c != nil && c.Tag == tag {
			out = append(out, c)
		}
	}
	return out
}

// String returns a compact human-readable representation, useful in logs
// and tests. Not stable for protocol use.
func (n *Node) String() string {
	if n == nil {
		return "<nil>"
	}
	var b strings.Builder
	b.WriteByte('<')
	b.WriteString(n.Tag)
	for k, v := range n.Attrs {
		b.WriteByte(' ')
		b.WriteString(k)
		b.WriteByte('=')
		b.WriteString(strconv.Quote(v))
	}
	if !n.HasChildren() && len(n.Data) == 0 {
		b.WriteString("/>")
		return b.String()
	}
	b.WriteByte('>')
	for _, c := range n.Children {
		b.WriteString(c.String())
	}
	if len(n.Data) > 0 {
		fmt.Fprintf(&b, "%d bytes", len(n.Data))
	}
	b.WriteString("</")
	b.WriteString(n.Tag)
	b.WriteByte('>')
	return b.String()
}

// Encode serializes a node tree to its on-wire form.
//
// If compress is true the body is zlib-compressed and the DEFLATE flag is
// set; this is what the server expects for inbound stanzas larger than a
// threshold. Outbound stanzas typically pass compress=false.
func Encode(n *Node, compress bool) ([]byte, error) {
	if n == nil {
		return nil, errors.New("binxmpp: encode nil node")
	}
	body := &bytes.Buffer{}
	if err := writeNode(body, n); err != nil {
		return nil, err
	}
	if !compress {
		out := make([]byte, 0, body.Len()+1)
		out = append(out, 0)
		out = append(out, body.Bytes()...)
		return out, nil
	}
	var compressed bytes.Buffer
	zw := zlib.NewWriter(&compressed)
	if _, err := zw.Write(body.Bytes()); err != nil {
		return nil, err
	}
	if err := zw.Close(); err != nil {
		return nil, err
	}
	out := make([]byte, 0, compressed.Len()+1)
	out = append(out, FlagDeflate)
	out = append(out, compressed.Bytes()...)
	return out, nil
}

// Decode parses a single node tree from buf. The first byte is the flag byte.
func Decode(buf []byte) (*Node, error) {
	if len(buf) == 0 {
		return nil, errors.New("binxmpp: empty input")
	}
	flags := buf[0]
	if flags&FlagSegmented != 0 {
		return nil, errors.New("binxmpp: segmented stanza must be reassembled before decode")
	}
	body := buf[1:]
	if flags&FlagDeflate != 0 {
		zr, err := zlib.NewReader(bytes.NewReader(body))
		if err != nil {
			return nil, fmt.Errorf("binxmpp: zlib: %w", err)
		}
		decoded, err := io.ReadAll(zr)
		_ = zr.Close()
		if err != nil {
			return nil, fmt.Errorf("binxmpp: zlib decompress: %w", err)
		}
		body = decoded
	}
	r := &reader{buf: body}
	return readNode(r)
}

// ---------------------------------------------------------------------------
// Encoder
// ---------------------------------------------------------------------------

func writeNode(w *bytes.Buffer, n *Node) error {
	x := 1 + 2*len(n.Attrs)
	if n.Data != nil {
		x++
	}
	if n.HasChildren() {
		x++
	}
	writeListStart(w, x)
	if err := writeString(w, n.Tag, false); err != nil {
		return err
	}
	// Iterate attrs in sorted-key order so encoding is deterministic.
	// Order is not protocol-significant — the server treats stanzas like XML.
	keys := make([]string, 0, len(n.Attrs))
	for k := range n.Attrs {
		keys = append(keys, k)
	}
	if len(keys) > 1 {
		sortStrings(keys)
	}
	for _, k := range keys {
		if err := writeString(w, k, false); err != nil {
			return err
		}
		if err := writeString(w, n.Attrs[k], true); err != nil {
			return err
		}
	}
	if n.Data != nil {
		writeBytes(w, n.Data, false)
	}
	if n.HasChildren() {
		writeListStart(w, len(n.Children))
		for _, c := range n.Children {
			if err := writeNode(w, c); err != nil {
				return err
			}
		}
	}
	return nil
}

func writeListStart(w *bytes.Buffer, i int) {
	switch {
	case i == 0:
		w.WriteByte(0)
	case i < 256:
		w.WriteByte(248)
		w.WriteByte(byte(i))
	default:
		w.WriteByte(249)
		w.WriteByte(byte(i >> 8))
		w.WriteByte(byte(i))
	}
}

// writeString picks the most compact encoding for s: dictionary token,
// double-byte secondary token, JID, packed-nibble, or raw bytes.
func writeString(w *bytes.Buffer, s string, packed bool) error {
	if s == "" {
		w.WriteByte(0)
		return nil
	}
	if idx, secondary, ok := lookupToken(s); ok {
		if !secondary {
			w.WriteByte(byte(idx))
			return nil
		}
		bank := idx / 256
		if bank > 3 {
			return fmt.Errorf("binxmpp: secondary token bank %d out of range", bank)
		}
		w.WriteByte(byte(236 + bank))
		w.WriteByte(byte(idx % 256))
		return nil
	}
	// JID?
	if at := strings.IndexByte(s, '@'); at >= 1 {
		user := s[:at]
		server := s[at+1:]
		return writeJid(w, user, server)
	}
	// Fallback: raw bytes (possibly packed).
	writeBytes(w, []byte(s), packed)
	return nil
}

// writeBytes emits a length-prefixed byte string, optionally trying the
// packed-hex (251) and packed-nibble (255) compact forms.
func writeBytes(w *bytes.Buffer, b []byte, packed bool) {
	size := len(b)
	switch {
	case size >= 0x100000:
		w.WriteByte(254)
		w.WriteByte(byte(size >> 24))
		w.WriteByte(byte(size >> 16))
		w.WriteByte(byte(size >> 8))
		w.WriteByte(byte(size))
		w.Write(b)
	case size >= 0x100:
		w.WriteByte(253)
		w.WriteByte(byte(size>>16) & 0x0F)
		w.WriteByte(byte(size >> 8))
		w.WriteByte(byte(size))
		w.Write(b)
	default:
		if packed && size < 128 {
			if buf := tryPack(255, b); buf != nil {
				w.WriteByte(255)
				writePackedHeader(w, len(b), len(buf))
				w.Write(buf)
				return
			}
			if buf := tryPack(251, b); buf != nil {
				w.WriteByte(251)
				writePackedHeader(w, len(b), len(buf))
				w.Write(buf)
				return
			}
		}
		w.WriteByte(252)
		w.WriteByte(byte(size))
		w.Write(b)
	}
}

func writePackedHeader(w *bytes.Buffer, origSize, packedLen int) {
	// high bit of length set when original length is odd (last nibble is filler).
	b := byte(packedLen & 0x7F)
	if origSize%2 == 1 {
		b |= 0x80
	}
	w.WriteByte(b)
}

// tryPack packs ASCII bytes into 4-bit nibbles for the 251 (hex 0-9A-F) or
// 255 (digits + '-' + '.') alphabets. Returns nil if any byte is not
// representable in the chosen alphabet.
func tryPack(kind byte, b []byte) []byte {
	out := make([]byte, (len(b)+1)/2)
	for i, c := range b {
		nibble := packByte(kind, c)
		if nibble == -1 {
			return nil
		}
		out[i/2] |= byte(nibble) << (4 * (1 - i%2))
	}
	if len(b)%2 == 1 {
		out[len(out)-1] |= 0x0F
	}
	return out
}

func packByte(kind byte, c byte) int {
	switch kind {
	case 251:
		switch {
		case c >= '0' && c <= '9':
			return int(c - '0')
		case c >= 'A' && c <= 'F':
			return int(c - 'A' + 10)
		}
	case 255:
		switch {
		case c == '-':
			return 10
		case c == '.':
			return 11
		case c >= '0' && c <= '9':
			return int(c - '0')
		}
	}
	return -1
}

// writeJid writes a JID. Three forms:
//   - user:device@server         → 247 + variant + device + user
//   - user@server                → 250 + user-string + server-string
//   - @server                    → 250 + 0 token   + server-string
func writeJid(w *bytes.Buffer, user, server string) error {
	if strings.Contains(user, ":") {
		parts := strings.SplitN(user, ":", 2)
		deviceStr := parts[1]
		dev, err := strconv.Atoi(deviceStr)
		if err != nil {
			return fmt.Errorf("binxmpp: bad device in jid: %q", user)
		}
		if dev < 0 || dev > 255 {
			return fmt.Errorf("binxmpp: device %d outside 0..255", dev)
		}
		userPart := parts[0]
		// Python encoder additionally strips a "." suffix from non-lid users.
		if server != "lid" {
			if dot := strings.IndexByte(userPart, '.'); dot >= 0 {
				userPart = userPart[:dot]
			}
		}
		w.WriteByte(247)
		if server == "lid" {
			w.WriteByte(1)
		} else {
			w.WriteByte(0)
		}
		w.WriteByte(byte(dev))
		return writeString(w, userPart, true)
	}
	if server == "lid" {
		w.WriteByte(247)
		w.WriteByte(1)
		w.WriteByte(0)
		return writeString(w, user, true)
	}
	w.WriteByte(250)
	if user == "" {
		w.WriteByte(0)
	} else {
		if err := writeString(w, user, true); err != nil {
			return err
		}
	}
	return writeString(w, server, false)
}

// ---------------------------------------------------------------------------
// Decoder
// ---------------------------------------------------------------------------

type reader struct {
	buf []byte
	pos int
}

func (r *reader) more(n int) bool { return r.pos+n <= len(r.buf) }

func (r *reader) read(n int) ([]byte, error) {
	if !r.more(n) {
		return nil, fmt.Errorf("binxmpp: short read: want %d at offset %d, have %d", n, r.pos, len(r.buf)-r.pos)
	}
	out := r.buf[r.pos : r.pos+n]
	r.pos += n
	return out, nil
}

func (r *reader) u8() (byte, error) {
	b, err := r.read(1)
	if err != nil {
		return 0, err
	}
	return b[0], nil
}

func readNode(r *reader) (*Node, error) {
	tok, err := r.u8()
	if err != nil {
		return nil, err
	}
	size, err := readListSize(r, tok)
	if err != nil {
		return nil, err
	}
	tagTok, err := r.u8()
	if err != nil {
		return nil, err
	}
	if tagTok == 2 {
		return nil, nil
	}
	tag, err := readString(r, tagTok)
	if err != nil {
		return nil, err
	}
	if size == 0 || tag == "" {
		return nil, errors.New("binxmpp: node with empty list/tag")
	}
	attrCount := (size - 2 + size%2) / 2
	attrs, err := readAttrs(r, attrCount)
	if err != nil {
		return nil, err
	}
	n := &Node{Tag: tag, Attrs: attrs}
	if size%2 == 1 {
		return n, nil
	}
	next, err := r.u8()
	if err != nil {
		return nil, err
	}
	switch {
	case isListTag(next):
		children, err := readList(r, next)
		if err != nil {
			return nil, err
		}
		n.Children = children
	case next == 252:
		sz, err := r.u8()
		if err != nil {
			return nil, err
		}
		b, err := r.read(int(sz))
		if err != nil {
			return nil, err
		}
		n.Data = append([]byte(nil), b...)
	case next == 253:
		b3, err := r.read(3)
		if err != nil {
			return nil, err
		}
		sz := (int(b3[0]&0x0F) << 16) | (int(b3[1]) << 8) | int(b3[2])
		b, err := r.read(sz)
		if err != nil {
			return nil, err
		}
		n.Data = append([]byte(nil), b...)
	case next == 254:
		b4, err := r.read(4)
		if err != nil {
			return nil, err
		}
		sz := (int(b4[0]&0x7F) << 24) | (int(b4[1]) << 16) | (int(b4[2]) << 8) | int(b4[3])
		b, err := r.read(sz)
		if err != nil {
			return nil, err
		}
		n.Data = append([]byte(nil), b...)
	case next == 251 || next == 255:
		s, err := readPacked(r, next)
		if err != nil {
			return nil, err
		}
		n.Data = []byte(s)
	default:
		s, err := readString(r, next)
		if err != nil {
			return nil, err
		}
		n.Data = []byte(s)
	}
	return n, nil
}

func readList(r *reader, tok byte) ([]*Node, error) {
	size, err := readListSize(r, tok)
	if err != nil {
		return nil, err
	}
	out := make([]*Node, 0, size)
	for i := 0; i < size; i++ {
		child, err := readNode(r)
		if err != nil {
			return nil, err
		}
		out = append(out, child)
	}
	return out, nil
}

func readListSize(r *reader, tok byte) (int, error) {
	switch tok {
	case 0:
		return 0, nil
	case 248:
		b, err := r.u8()
		if err != nil {
			return 0, err
		}
		return int(b), nil
	case 249:
		b, err := r.read(2)
		if err != nil {
			return 0, err
		}
		return (int(b[0]) << 8) | int(b[1]), nil
	}
	return 0, fmt.Errorf("binxmpp: invalid list-size token %d", tok)
}

func readAttrs(r *reader, n int) (map[string]string, error) {
	if n <= 0 {
		return nil, nil
	}
	out := make(map[string]string, n)
	for i := 0; i < n; i++ {
		ktok, err := r.u8()
		if err != nil {
			return nil, err
		}
		k, err := readString(r, ktok)
		if err != nil {
			return nil, err
		}
		vtok, err := r.u8()
		if err != nil {
			return nil, err
		}
		v, err := readString(r, vtok)
		if err != nil {
			return nil, err
		}
		out[k] = v
	}
	return out, nil
}

func readString(r *reader, tok byte) (string, error) {
	switch {
	case tok == 0:
		return "", nil
	case tok > 2 && tok < 236:
		return tokenAt(int(tok))
	case tok == 236 || tok == 237 || tok == 238 || tok == 239:
		idx, err := r.u8()
		if err != nil {
			return "", err
		}
		return secondaryTokenAt(int(tok-236)*256 + int(idx))
	case tok == 247:
		variant, err := r.u8()
		if err != nil {
			return "", err
		}
		dev, err := r.u8()
		if err != nil {
			return "", err
		}
		userTok, err := r.u8()
		if err != nil {
			return "", err
		}
		user, err := readString(r, userTok)
		if err != nil {
			return "", err
		}
		if variant == 1 {
			if dev == 0 {
				return user + "@lid", nil
			}
			return fmt.Sprintf("%s:%d@lid", user, dev), nil
		}
		// variant 0: agent-or-device JID on s.whatsapp.net (mirror Python)
		return fmt.Sprintf("%s.%d:%d@s.whatsapp.net", user, variant, dev), nil
	case tok == 250:
		userTok, err := r.u8()
		if err != nil {
			return "", err
		}
		user, err := readString(r, userTok)
		if err != nil {
			return "", err
		}
		serverTok, err := r.u8()
		if err != nil {
			return "", err
		}
		server, err := readString(r, serverTok)
		if err != nil {
			return "", err
		}
		if user == "" {
			return server, nil
		}
		return user + "@" + server, nil
	case tok == 251 || tok == 255:
		return readPacked(r, tok)
	case tok == 252:
		sz, err := r.u8()
		if err != nil {
			return "", err
		}
		b, err := r.read(int(sz))
		if err != nil {
			return "", err
		}
		return string(b), nil
	case tok == 253:
		b3, err := r.read(3)
		if err != nil {
			return "", err
		}
		sz := (int(b3[0]&0x0F) << 16) | (int(b3[1]) << 8) | int(b3[2])
		b, err := r.read(sz)
		if err != nil {
			return "", err
		}
		return string(b), nil
	case tok == 254:
		b4, err := r.read(4)
		if err != nil {
			return "", err
		}
		sz := (int(b4[0]&0x7F) << 24) | (int(b4[1]) << 16) | (int(b4[2]) << 8) | int(b4[3])
		b, err := r.read(sz)
		if err != nil {
			return "", err
		}
		return string(b), nil
	}
	return "", fmt.Errorf("binxmpp: unknown string token %d", tok)
}

func readPacked(r *reader, kind byte) (string, error) {
	hdr, err := r.u8()
	if err != nil {
		return "", err
	}
	ignoreLast := hdr&0x80 != 0
	size := int(hdr & 0x7F)
	b, err := r.read(size)
	if err != nil {
		return "", err
	}
	nNibbles := size * 2
	if ignoreLast {
		nNibbles--
	}
	out := make([]byte, 0, nNibbles)
	for i := 0; i < nNibbles; i++ {
		nib := (b[i/2] >> (4 * (1 - byte(i%2)))) & 0x0F
		c, err := unpackByte(kind, nib)
		if err != nil {
			return "", err
		}
		out = append(out, c)
	}
	return string(out), nil
}

func unpackByte(kind byte, n byte) (byte, error) {
	switch kind {
	case 251:
		if n < 10 {
			return '0' + n, nil
		}
		if n < 16 {
			return 'A' + (n - 10), nil
		}
	case 255:
		if n < 10 {
			return '0' + n, nil
		}
		if n == 10 {
			return '-', nil
		}
		if n == 11 {
			return '.', nil
		}
	}
	return 0, fmt.Errorf("binxmpp: bad %d-packed nibble %d", kind, n)
}

func isListTag(b byte) bool { return b == 0 || b == 248 || b == 249 }

func tokenAt(i int) (string, error) {
	if i < 0 || i >= len(PrimaryDict) {
		return "", fmt.Errorf("binxmpp: primary token %d out of range", i)
	}
	return PrimaryDict[i], nil
}

func secondaryTokenAt(i int) (string, error) {
	if i < 0 || i >= len(SecondaryDict) {
		return "", fmt.Errorf("binxmpp: secondary token %d out of range", i)
	}
	return SecondaryDict[i], nil
}

// ---------------------------------------------------------------------------
// Token lookup (encode side)
// ---------------------------------------------------------------------------

var (
	primaryIndex   map[string]int
	secondaryIndex map[string]int
)

func init() {
	primaryIndex = make(map[string]int, len(PrimaryDict))
	for i, s := range PrimaryDict {
		if i < 3 || s == "" {
			continue
		}
		primaryIndex[s] = i
	}
	secondaryIndex = make(map[string]int, len(SecondaryDict))
	for i, s := range SecondaryDict {
		if s == "" {
			continue
		}
		secondaryIndex[s] = i
	}
}

func lookupToken(s string) (idx int, secondary, ok bool) {
	if i, ok := primaryIndex[s]; ok {
		return i, false, true
	}
	if i, ok := secondaryIndex[s]; ok {
		return i, true, true
	}
	return 0, false, false
}
