package ui

import (
	"bufio"
	"bytes"
	"strconv"
	"strings"
	"testing"
)

func TestEncodeLSPMessage_PrependsContentLength(t *testing.T) {
	got := string(encodeLSPMessage([]byte(`{"jsonrpc":"2.0"}`)))
	want := "Content-Length: 17\r\n\r\n" + `{"jsonrpc":"2.0"}`
	if got != want {
		t.Fatalf("encodeLSPMessage = %q, want %q", got, want)
	}
}

func TestReadLSPMessage_ParsesFramedBody(t *testing.T) {
	body := `{"id":1,"result":null}`
	stream := "Content-Length: " + strconv.Itoa(len(body)) + "\r\n\r\n" + body
	got, err := readLSPMessage(bufio.NewReader(strings.NewReader(stream)))
	if err != nil {
		t.Fatalf("readLSPMessage error: %v", err)
	}
	if string(got) != body {
		t.Fatalf("readLSPMessage = %q, want %q", got, body)
	}
}

func TestReadLSPMessage_RoundTrip(t *testing.T) {
	// A frame encoded for stdin must read back byte-for-byte as the body.
	body := []byte(`{"method":"textDocument/didOpen","params":{"x":"héllo"}}`)
	framed := encodeLSPMessage(body)
	got, err := readLSPMessage(bufio.NewReader(bytes.NewReader(framed)))
	if err != nil {
		t.Fatalf("round-trip error: %v", err)
	}
	if !bytes.Equal(got, body) {
		t.Fatalf("round-trip = %q, want %q", got, body)
	}
}

func TestReadLSPMessage_CaseInsensitiveHeaderAndExtraHeaders(t *testing.T) {
	body := `{"ok":true}`
	stream := "content-length: " + strconv.Itoa(len(body)) + "\r\n" +
		"Content-Type: application/vscode-jsonrpc; charset=utf-8\r\n\r\n" + body
	got, err := readLSPMessage(bufio.NewReader(strings.NewReader(stream)))
	if err != nil {
		t.Fatalf("readLSPMessage error: %v", err)
	}
	if string(got) != body {
		t.Fatalf("readLSPMessage = %q, want %q", got, body)
	}
}

func TestReadLSPMessage_MissingContentLength(t *testing.T) {
	_, err := readLSPMessage(bufio.NewReader(strings.NewReader("X-Foo: bar\r\n\r\n{}")))
	if err == nil {
		t.Fatal("expected error for message without Content-Length")
	}
}

func TestReadLSPMessage_RejectsOversizedAndNegative(t *testing.T) {
	huge := "Content-Length: " + strconv.Itoa(maxLSPMessageBytes+1) + "\r\n\r\n"
	if _, err := readLSPMessage(bufio.NewReader(strings.NewReader(huge))); err == nil {
		t.Fatal("expected error for oversized Content-Length")
	}
	neg := "Content-Length: -5\r\n\r\nxxxxx"
	if _, err := readLSPMessage(bufio.NewReader(strings.NewReader(neg))); err == nil {
		t.Fatal("expected error for negative Content-Length")
	}
}
