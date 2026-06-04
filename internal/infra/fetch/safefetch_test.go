package fetch

import (
	"context"
	"errors"
	"net"
	"testing"
)

func TestIsBlockedIP(t *testing.T) {
	tests := []struct {
		ip      string
		blocked bool
	}{
		{"127.0.0.1", true},        // loopback
		{"::1", true},              // loopback v6
		{"10.0.0.5", true},         // private
		{"172.16.3.4", true},       // private
		{"192.168.1.1", true},      // private
		{"169.254.169.254", true},  // cloud metadata (link-local)
		{"fe80::1", true},          // link-local v6
		{"fc00::1", true},          // unique local v6
		{"0.0.0.0", true},          // unspecified
		{"100.64.0.1", true},       // CGNAT
		{"8.8.8.8", false},         // public
		{"1.1.1.1", false},         // public
		{"93.184.216.34", false},   // public (example.com)
	}
	for _, tc := range tests {
		ip := net.ParseIP(tc.ip)
		if ip == nil {
			t.Fatalf("bad test IP %q", tc.ip)
		}
		if got := isBlockedIP(ip); got != tc.blocked {
			t.Errorf("isBlockedIP(%s) = %v, want %v", tc.ip, got, tc.blocked)
		}
	}
}

func TestGetRejectsBadScheme(t *testing.T) {
	f := New(Options{})
	for _, u := range []string{"file:///etc/passwd", "gopher://x", "ftp://x/y", "data:text/plain,hi"} {
		if _, err := f.Get(context.Background(), u); !errors.Is(err, ErrBadScheme) {
			t.Errorf("Get(%q) err = %v, want ErrBadScheme", u, err)
		}
	}
}

func TestGetRejectsLoopback(t *testing.T) {
	f := New(Options{})
	// Connecting to localhost must be blocked by the dialer Control hook.
	_, err := f.Get(context.Background(), "http://127.0.0.1:80/")
	if err == nil {
		t.Fatal("expected loopback fetch to be blocked, got nil error")
	}
}
