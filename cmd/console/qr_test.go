package main

import (
	"bytes"
	"net"
	"strings"
	"testing"
)

func TestAddrPort(t *testing.T) {
	cases := map[string]string{
		":8080":             "8080",
		"127.0.0.1:8080":    "8080",
		"0.0.0.0:9000":      "9000",
		"192.168.1.5:18080": "18080",
		"garbage":           "8080",
		"":                  "8080",
	}
	for in, want := range cases {
		if got := addrPort(in); got != want {
			t.Errorf("addrPort(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestIsLoopback(t *testing.T) {
	cases := map[string]bool{
		"127.0.0.1:8080": true,
		"localhost:8080": true,
		"[::1]:8080":     true,
		":8080":          false, // all interfaces
		"0.0.0.0:8080":   false,
		"192.168.1.5:80": false,
	}
	for in, want := range cases {
		if got := isLoopback(in); got != want {
			t.Errorf("isLoopback(%q) = %v, want %v", in, got, want)
		}
	}
}

func TestShowQR(t *testing.T) {
	var buf bytes.Buffer
	if err := showQR(&buf, "http://192.168.1.42:8080", true, "8080"); err != nil {
		t.Fatalf("showQR: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "http://192.168.1.42:8080") {
		t.Errorf("output missing target URL:\n%s", out)
	}
	if !strings.Contains(out, "same Wi-Fi") {
		t.Errorf("output missing same-network guidance")
	}
	if !strings.Contains(out, "loopback") {
		t.Errorf("loopback warning missing when loopback=true")
	}
	// A user-supplied URL (port "") must omit the LAN guidance.
	var buf2 bytes.Buffer
	_ = showQR(&buf2, "https://x.example.com", false, "")
	if strings.Contains(buf2.String(), "same Wi-Fi") {
		t.Errorf("user-URL output should not include LAN guidance")
	}
}

func TestLanIP(t *testing.T) {
	ip, err := lanIP()
	if err != nil {
		t.Skipf("no LAN IP available in this environment: %v", err)
	}
	if net.ParseIP(ip) == nil {
		t.Errorf("lanIP() = %q, not a valid IP", ip)
	}
}
