package main

import (
	"bytes"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/cyfdecyf/bufio"
)

// newTestServerConn builds a serverConn whose buffered reader is fed from the
// given raw bytes via a net.Pipe (server side). The client side is closed so
// reads only consume the provided data.
func newTestServerConn(raw string) *serverConn {
	cli, srv := net.Pipe()
	go func() {
		srv.Write([]byte(raw))
		srv.Close()
	}()
	sv := newServerConn(cli, "example.com:80", newVisitCnt(0, 0))
	sv.initBuf()
	return sv
}

func TestParseResponseShortStatusLine(t *testing.T) {
	// Bug #7: a status line shorter than "HTTP/1.x" used to panic on slice.
	errl = false
	defer func() { errl = true }()

	// "H 200 OK\r\n" -> proto == "H" (len 1), must error not panic.
	sv := newTestServerConn("H 200 OK\r\nContent-Length: 0\r\n\r\n")
	r := &Request{URL: &URL{HostPort: "example.com:80"}}
	rp := &Response{}
	// readFinalResponse will parse status line then header; ensure no panic.
	err := parseResponse(sv, r, rp)
	if err == nil {
		t.Fatal("expected error for malformed short status line, got nil")
	}
}

func TestParseResponseNormal200(t *testing.T) {
	errl = false
	defer func() { errl = true }()

	sv := newTestServerConn("HTTP/1.1 200 OK\r\nContent-Length: 0\r\n\r\n")
	r := &Request{URL: &URL{HostPort: "example.com:80"}}
	rp := &Response{}
	if err := parseResponse(sv, r, rp); err != nil {
		t.Fatalf("unexpected error parsing 200: %v", err)
	}
	if rp.Status != 200 {
		t.Fatalf("status = %d, want 200", rp.Status)
	}
}

func TestParseResponseManyContinueBounded(t *testing.T) {
	// Bug #6: many interim "100 Continue" responses must be skipped but the
	// recursion must be bounded (was an unbounded stack overflow).
	errl = false
	defer func() { errl = true }()

	var b strings.Builder
	for i := 0; i < 50; i++ {
		b.WriteString("HTTP/1.1 100 Continue\r\n\r\n")
	}
	b.WriteString("HTTP/1.1 200 OK\r\nContent-Length: 0\r\n\r\n")
	sv := newTestServerConn(b.String())
	r := &Request{URL: &URL{HostPort: "example.com:80"}}
	rp := &Response{}
	// // Proof: readFinalResponse loops (not recurses) and aborts after
	// maxContinue interim responses, returning an error instead of overflowing.
	err := parseResponse(sv, r, rp)
	if err == nil {
		t.Fatal("expected error after too many 100-continue responses, got nil")
	}
}

func TestParseResponseContinueThenFinal(t *testing.T) {
	// A single interim 100 Continue followed by 200 must succeed.
	errl = false
	defer func() { errl = true }()

	raw := "HTTP/1.1 100 Continue\r\n\r\nHTTP/1.1 200 OK\r\nContent-Length: 0\r\n\r\n"
	sv := newTestServerConn(raw)
	r := &Request{URL: &URL{HostPort: "example.com:80"}}
	rp := &Response{}
	if err := parseResponse(sv, r, rp); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if rp.Status != 200 {
		t.Fatalf("status = %d, want 200", rp.Status)
	}
}

// Ensure bufio slice helpers used by sendBodyChunked still compile against the
// vendored bufio fork (regression guard for the import).
func TestBufioSliceHelpers(t *testing.T) {
	rd := bufio.NewReader(bytes.NewReader([]byte("abc")))
	if rd.Buffered() != 0 {
		t.Fatal("unexpected buffered")
	}
	_ = time.Second
}
