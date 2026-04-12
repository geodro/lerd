package ui

import (
	"bufio"
	"crypto/sha1"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"io"
	"net"
	"net/http"
	"strings"
)

// Hand-rolled RFC6455 WebSocket helpers. Scope: lerd-ui only ever sends JSON
// text frames to browser clients, so this supports the subset we need —
// server-side handshake, unmasked text frames out, masked frames in, close
// and ping/pong control frames. No binary, no fragmentation, no compression.
//
// Keeping it in-tree avoids adding a websocket dependency for what is a
// few dozen lines of straightforward protocol work.

const wsGUID = "258EAFA5-E914-47DA-95CA-C5AB0DC85B11"

// Opcodes we care about.
const (
	wsOpText  = 0x1
	wsOpClose = 0x8
	wsOpPing  = 0x9
	wsOpPong  = 0xA
)

// wsConn wraps a hijacked net.Conn with the buffered reader from the
// handshake so WriteText and ReadFrame can share the same underlying stream.
type wsConn struct {
	conn net.Conn
	br   *bufio.Reader
}

// wsUpgrade performs the RFC6455 handshake and returns a wsConn ready for
// ReadFrame / WriteText. The http.ResponseWriter must support Hijack.
func wsUpgrade(w http.ResponseWriter, r *http.Request) (*wsConn, error) {
	if !strings.EqualFold(r.Header.Get("Upgrade"), "websocket") {
		return nil, errors.New("missing Upgrade: websocket header")
	}
	if !strings.Contains(strings.ToLower(r.Header.Get("Connection")), "upgrade") {
		return nil, errors.New("missing Connection: upgrade header")
	}
	key := r.Header.Get("Sec-WebSocket-Key")
	if key == "" {
		return nil, errors.New("missing Sec-WebSocket-Key")
	}
	h, ok := w.(http.Hijacker)
	if !ok {
		return nil, errors.New("response writer does not support hijack")
	}
	conn, brw, err := h.Hijack()
	if err != nil {
		return nil, err
	}
	sum := sha1.Sum([]byte(key + wsGUID))
	accept := base64.StdEncoding.EncodeToString(sum[:])
	resp := "HTTP/1.1 101 Switching Protocols\r\n" +
		"Upgrade: websocket\r\n" +
		"Connection: Upgrade\r\n" +
		"Sec-WebSocket-Accept: " + accept + "\r\n\r\n"
	if _, err := brw.Writer.WriteString(resp); err != nil {
		conn.Close()
		return nil, err
	}
	if err := brw.Writer.Flush(); err != nil {
		conn.Close()
		return nil, err
	}
	return &wsConn{conn: conn, br: brw.Reader}, nil
}

// WriteText sends a single unfragmented text frame.
func (c *wsConn) WriteText(payload []byte) error {
	return c.writeFrame(wsOpText, payload)
}

// WritePong sends a pong frame in reply to a ping, echoing the payload.
func (c *wsConn) WritePong(payload []byte) error {
	return c.writeFrame(wsOpPong, payload)
}

// WriteClose sends a close frame with no payload.
func (c *wsConn) WriteClose() error {
	return c.writeFrame(wsOpClose, nil)
}

func (c *wsConn) writeFrame(op byte, payload []byte) error {
	header := make([]byte, 2, 10)
	header[0] = 0x80 | op // FIN | opcode
	n := len(payload)
	switch {
	case n < 126:
		header[1] = byte(n)
	case n <= 0xFFFF:
		header[1] = 126
		header = binary.BigEndian.AppendUint16(header, uint16(n))
	default:
		header[1] = 127
		header = binary.BigEndian.AppendUint64(header, uint64(n))
	}
	if _, err := c.conn.Write(header); err != nil {
		return err
	}
	if n > 0 {
		if _, err := c.conn.Write(payload); err != nil {
			return err
		}
	}
	return nil
}

// Close shuts down the underlying connection.
func (c *wsConn) Close() error { return c.conn.Close() }

// ReadFrame reads one frame and returns its opcode and payload. Client frames
// are always masked per RFC6455; the mask is applied before return.
func (c *wsConn) ReadFrame() (op byte, payload []byte, err error) {
	var hdr [2]byte
	if _, err = io.ReadFull(c.br, hdr[:]); err != nil {
		return
	}
	op = hdr[0] & 0x0F
	masked := hdr[1]&0x80 != 0
	plen := int(hdr[1] & 0x7F)
	switch plen {
	case 126:
		var ext [2]byte
		if _, err = io.ReadFull(c.br, ext[:]); err != nil {
			return
		}
		plen = int(binary.BigEndian.Uint16(ext[:]))
	case 127:
		var ext [8]byte
		if _, err = io.ReadFull(c.br, ext[:]); err != nil {
			return
		}
		plen = int(binary.BigEndian.Uint64(ext[:]))
	}
	var mask [4]byte
	if masked {
		if _, err = io.ReadFull(c.br, mask[:]); err != nil {
			return
		}
	}
	payload = make([]byte, plen)
	if _, err = io.ReadFull(c.br, payload); err != nil {
		return
	}
	if masked {
		for i := range payload {
			payload[i] ^= mask[i%4]
		}
	}
	return
}
