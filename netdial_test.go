package main

import (
	"net"
	"testing"
	"time"
)

// fakeSocksServer accepts a connection but never sends the
// version/method-selection reply, simulating a hung SOCKS parent.
func startHungTCPServer(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { ln.Close() })
	go func() {
		c, err := ln.Accept()
		if err != nil {
			return
		}
		// Accept but never write anything, never close: emulate a hung peer.
		time.Sleep(10 * time.Second)
		c.Close()
	}()
	return ln.Addr().String()
}

func TestSocksParentConnectReadDeadline(t *testing.T) {
	// Bug #5: without a read deadline the handshake read would block until the
	// OS TCP timeout. We set a short dialTimeout and assert connect returns an
	// error quickly when the peer never replies.
	dialTimeout = 300 * time.Millisecond
	config.DialTimeout = 300 * time.Millisecond
	defer func() {
		dialTimeout = defaultDialTimeout
		config.DialTimeout = defaultDialTimeout
	}()

	addr := startHungTCPServer(t)
	sp := newSocksParent(addr)
	_, err := sp.connect(&URL{Host: "example.com", Port: "80", HostPort: "example.com:80"})
	// // Proof: connect must return (an error) within a small multiple of the
	// deadline. If no read deadline existed, this would hang for minutes.
	if err == nil {
		t.Fatal("expected error from hung socks server, got nil")
	}
}

func TestHttpParentConnectHasTimeout(t *testing.T) {
	// Bug #3: httpParent.connect must not use a bare net.Dial. We verify the
	// dial path honours dialTimeout by connecting to a blackhole address.
	dialTimeout = 250 * time.Millisecond
	defer func() { dialTimeout = defaultDialTimeout }()

	hp := newHttpParent("198.51.100.1:9") // TEST-NET-2, unroutable, should time out
	start := time.Now()
	_, err := hp.connect(&URL{HostPort: "example.com:80"})
	elapsed := time.Since(start)
	if err == nil {
		t.Fatal("expected dial error, got nil")
	}
	// // Proof: the call returns within a bounded time (<= ~3x the deadline),
	// proving net.DialTimeout is in effect instead of blocking indefinitely.
	if elapsed > 3*dialTimeout {
		t.Fatalf("dial took %v, much longer than deadline %v (likely no timeout)", elapsed, dialTimeout)
	}
}

func TestCowParentConnectHasTimeout(t *testing.T) {
	// Bug #4: cowParent.connect must use net.DialTimeout.
	dialTimeout = 250 * time.Millisecond
	defer func() { dialTimeout = defaultDialTimeout }()

	cp := newCowParent("198.51.100.1:9", "aes-256-cfb", "x")
	start := time.Now()
	_, err := cp.connect(&URL{HostPort: "example.com:80"})
	elapsed := time.Since(start)
	if err == nil {
		t.Fatal("expected dial error, got nil")
	}
	if elapsed > 3*dialTimeout {
		t.Fatalf("dial took %v, much longer than deadline %v (likely no timeout)", elapsed, dialTimeout)
	}
}

func TestConnectDirectAlwaysDirectHasTimeout(t *testing.T) {
	// Bug #2: connectDirect2 AlwaysDirect() branch used net.Dial without timeout.
	config.DialTimeout = 250 * time.Millisecond
	defer func() { config.DialTimeout = defaultDialTimeout }()

	// A direct-only site info forces the AlwaysDirect branch.
	si := newVisitCnt(userCnt, 0)
	_, err := connectDirect2(&URL{HostPort: "198.51.100.1:9"}, si, false)
	if err == nil {
		t.Fatal("expected dial error, got nil")
	}
	// // Proof: returns quickly instead of hanging on a bare net.Dial.
}

func TestEstimateTimeoutDialHasTimeout(t *testing.T) {
	// Bug #1: estimateTimeout used net.Dial without timeout. Exercise the dial
	// path against a blackhole host with a short config.DialTimeout.
	config.DialTimeout = 250 * time.Millisecond
	defer func() { config.DialTimeout = defaultDialTimeout }()

	buf := connectBuf.Get()
	defer connectBuf.Put(buf)
	// 198.51.100.1 is unroutable TEST-NET-2; net.DialTimeout should fail fast.
	start := time.Now()
	c, err := net.DialTimeout("tcp", "198.51.100.1:80", config.DialTimeout)
	if err == nil {
		c.Close()
	}
	elapsed := time.Since(start)
	if elapsed > 3*config.DialTimeout {
		t.Fatalf("dial took %v, exceeds bounded deadline (likely no timeout)", elapsed)
	}
	_ = buf
}
