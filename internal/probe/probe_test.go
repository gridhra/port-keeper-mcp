package probe

import (
	"net"
	"testing"
)

func TestListening(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	if !Listening(port) {
		t.Fatalf("port %d has a listener but Listening returned false", port)
	}
	_ = ln.Close()
	if Listening(port) {
		t.Fatalf("port %d was closed but Listening returned true", port)
	}
	// Wildcard listener must be detected too.
	ln2, err := net.Listen("tcp", ":0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln2.Close()
	if !Listening(ln2.Addr().(*net.TCPAddr).Port) {
		t.Fatal("wildcard listener not detected")
	}
}

func TestListeningIPv6Only(t *testing.T) {
	ln, err := net.Listen("tcp6", "[::1]:0")
	if err != nil {
		t.Skip("IPv6 loopback not available")
	}
	defer ln.Close()
	port := ln.Addr().(*net.TCPAddr).Port
	if !Listening(port) {
		t.Fatalf("IPv6-only listener on %d not detected", port)
	}
}

func TestProbeLeavesNothingOpen(t *testing.T) {
	ln, _ := net.Listen("tcp", "127.0.0.1:0")
	port := ln.Addr().(*net.TCPAddr).Port
	ln.Close()
	for i := 0; i < 3; i++ {
		if Listening(port) {
			t.Fatalf("probe %d left port %d listening", i, port)
		}
	}
}
