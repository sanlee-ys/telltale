package eventsink

// A minimal server-side WebSocket (RFC 6455), hand-rolled for the same
// reason internal/sqlite hand-rolls its file reader: this repo takes no
// dependency for a path it can carry in a page of checked stdlib code. The
// sink needs exactly one direction of one frame type — server-to-client text
// — plus the handshake, pong replies, and a clean close. Fragmentation,
// extensions, subprotocols and client-to-server payloads have no consumer
// here and are not implemented: a fragmented or oversized client frame closes
// the connection rather than being half-understood.

import (
	"bufio"
	"crypto/sha1"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
)

// wsGUID is the fixed key-digest suffix RFC 6455 §4.2.2 prescribes.
const wsGUID = "258EAFA5-E914-47DA-95CA-C5AB0DC85B11"

// maxClientFrame caps what a client may send. Clients of this stream have
// nothing to say beyond control frames; the cap only has to fit a close
// reason or a ping payload (both ≤125 by spec), with slack for a chatty but
// honest client.
const maxClientFrame = 4096

// upgrade performs the server side of the opening handshake and hands back
// the raw connection. On refusal it writes the HTTP error itself, so the
// caller's only job on error is to stop.
func upgrade(w http.ResponseWriter, r *http.Request) (net.Conn, error) {
	if r.Method != http.MethodGet {
		http.Error(w, "websocket handshake wants GET", http.StatusMethodNotAllowed)
		return nil, errors.New("not GET")
	}
	if !headerHasToken(r.Header, "Connection", "upgrade") || !headerHasToken(r.Header, "Upgrade", "websocket") {
		http.Error(w, "want a websocket upgrade", http.StatusBadRequest)
		return nil, errors.New("not an upgrade")
	}
	key := r.Header.Get("Sec-WebSocket-Key")
	if key == "" {
		http.Error(w, "missing Sec-WebSocket-Key", http.StatusBadRequest)
		return nil, errors.New("missing key")
	}
	hj, ok := w.(http.Hijacker)
	if !ok {
		http.Error(w, "connection cannot be hijacked", http.StatusInternalServerError)
		return nil, errors.New("no hijacker")
	}
	conn, rw, err := hj.Hijack()
	if err != nil {
		return nil, err
	}
	sum := sha1.Sum([]byte(key + wsGUID))
	accept := base64.StdEncoding.EncodeToString(sum[:])
	resp := "HTTP/1.1 101 Switching Protocols\r\n" +
		"Upgrade: websocket\r\n" +
		"Connection: Upgrade\r\n" +
		"Sec-WebSocket-Accept: " + accept + "\r\n\r\n"
	if _, err := rw.WriteString(resp); err != nil {
		conn.Close()
		return nil, err
	}
	if err := rw.Flush(); err != nil {
		conn.Close()
		return nil, err
	}
	// The hijacked bufio reader may hold bytes the client pipelined after the
	// handshake; wrap so the frame reader sees them. bufferedConn keeps the
	// net.Conn interface the rest of the file speaks.
	return &bufferedConn{Conn: conn, r: rw.Reader}, nil
}

// headerHasToken reports whether a comma-separated header contains the token
// case-insensitively — "Connection: keep-alive, Upgrade" must match.
func headerHasToken(h http.Header, name, token string) bool {
	for _, v := range h.Values(name) {
		for _, part := range strings.Split(v, ",") {
			if strings.EqualFold(strings.TrimSpace(part), token) {
				return true
			}
		}
	}
	return false
}

type bufferedConn struct {
	net.Conn
	r *bufio.Reader
}

func (c *bufferedConn) Read(p []byte) (int, error) { return c.r.Read(p) }

// Frame opcodes (RFC 6455 §5.2), the ones this file speaks.
const (
	opText  = 0x1
	opClose = 0x8
	opPing  = 0x9
	opPong  = 0xA
)

// writeText sends one unmasked FIN text frame. Server frames are unmasked by
// spec; the length field widens at the two RFC thresholds.
func writeText(conn net.Conn, payload []byte) error {
	return writeFrame(conn, opText, payload)
}

func writeFrame(conn net.Conn, opcode byte, payload []byte) error {
	var header []byte
	first := byte(0x80 | opcode) // FIN set, no fragmentation ever sent
	switch n := len(payload); {
	case n <= 125:
		header = []byte{first, byte(n)}
	case n <= 0xFFFF:
		header = []byte{first, 126, 0, 0}
		binary.BigEndian.PutUint16(header[2:], uint16(n))
	default:
		header = make([]byte, 10)
		header[0], header[1] = first, 127
		binary.BigEndian.PutUint64(header[2:], uint64(n))
	}
	if _, err := conn.Write(header); err != nil {
		return err
	}
	_, err := conn.Write(payload)
	return err
}

// readUntilClose consumes client frames until a close frame, a protocol
// error, or a broken connection. It answers pings with pongs and discards
// everything else — the stream is one-directional and a client's text has no
// consumer. On a client close it echoes a close frame back, which is the
// closing handshake from this side.
func readUntilClose(conn net.Conn) {
	for {
		opcode, payload, err := readFrame(conn)
		if err != nil {
			return
		}
		switch opcode {
		case opClose:
			writeFrame(conn, opClose, payload)
			return
		case opPing:
			if err := writeFrame(conn, opPong, payload); err != nil {
				return
			}
		default:
			// Text, binary, pong: consumed and dropped.
		}
	}
}

// readFrame reads one client frame. Client frames MUST be masked (RFC 6455
// §5.1); an unmasked or fragmented or oversized frame is a protocol error
// and errors out, which closes the connection above.
func readFrame(conn net.Conn) (opcode byte, payload []byte, err error) {
	var head [2]byte
	if _, err = io.ReadFull(conn, head[:]); err != nil {
		return 0, nil, err
	}
	fin := head[0]&0x80 != 0
	opcode = head[0] & 0x0F
	if !fin {
		return 0, nil, errors.New("fragmented client frame")
	}
	masked := head[1]&0x80 != 0
	if !masked {
		return 0, nil, errors.New("unmasked client frame")
	}
	length := uint64(head[1] & 0x7F)
	switch length {
	case 126:
		var ext [2]byte
		if _, err = io.ReadFull(conn, ext[:]); err != nil {
			return 0, nil, err
		}
		length = uint64(binary.BigEndian.Uint16(ext[:]))
	case 127:
		var ext [8]byte
		if _, err = io.ReadFull(conn, ext[:]); err != nil {
			return 0, nil, err
		}
		length = binary.BigEndian.Uint64(ext[:])
	}
	if length > maxClientFrame {
		return 0, nil, fmt.Errorf("client frame of %d bytes exceeds the %d cap", length, maxClientFrame)
	}
	var mask [4]byte
	if _, err = io.ReadFull(conn, mask[:]); err != nil {
		return 0, nil, err
	}
	payload = make([]byte, length)
	if _, err = io.ReadFull(conn, payload); err != nil {
		return 0, nil, err
	}
	for i := range payload {
		payload[i] ^= mask[i%4]
	}
	return opcode, payload, nil
}
