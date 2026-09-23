package discovery

import (
	"net"
	"testing"
	"time"
)

func TestObserveBeaconAndSnapshot(t *testing.T) {
	l := New("192.168.32.100")
	now := time.Date(2026, 9, 23, 4, 30, 0, 0, time.UTC)
	l.now = func() time.Time { return now }

	if !l.observe(net.ParseIP("192.168.32.100"), []byte("PKGSENDER v1"), now.Add(-2*time.Second)) {
		t.Fatal("expected configured PS5 beacon to be accepted")
	}
	if !l.observe(net.ParseIP("192.168.32.101"), []byte("PKGSENDER future"), now.Add(-20*time.Second)) {
		t.Fatal("expected compatible beacon prefix to be accepted")
	}
	s := l.Snapshot()
	if s.Port != BeaconPort || s.ConfiguredIP != "192.168.32.100" {
		t.Fatalf("snapshot config=%+v", s)
	}
	if len(s.Consoles) != 2 {
		t.Fatalf("consoles=%d, want 2", len(s.Consoles))
	}
	if got := s.Consoles[0]; got.IP != "192.168.32.100" || !got.Configured || !got.Online {
		t.Fatalf("configured console=%+v", got)
	}
	if got := s.Consoles[1]; got.IP != "192.168.32.101" || got.Configured || got.Online {
		t.Fatalf("stale console=%+v", got)
	}
}

func TestObserveRejectsInvalidBeaconSources(t *testing.T) {
	l := New("192.168.32.100")
	now := time.Now()
	for _, tc := range []struct {
		name    string
		ip      net.IP
		payload []byte
	}{
		{name: "wrong magic", ip: net.ParseIP("192.168.32.100"), payload: []byte("OTHER v1")},
		{name: "loopback", ip: net.ParseIP("127.0.0.1"), payload: []byte("PKGSENDER v1")},
		{name: "ipv6", ip: net.ParseIP("2001:db8::1"), payload: []byte("PKGSENDER v1")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if l.observe(tc.ip, tc.payload, now) {
				t.Fatal("unexpected beacon acceptance")
			}
		})
	}
	if len(l.Snapshot().Consoles) != 0 {
		t.Fatal("rejected beacons must not appear in snapshot")
	}
}
