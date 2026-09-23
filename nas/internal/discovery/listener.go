package discovery

import (
	"bytes"
	"context"
	"errors"
	"net"
	"sort"
	"strings"
	"sync"
	"time"
)

const (
	BeaconPort  = 12801
	BeaconMagic = "PKGSENDER"
	onlineFor   = 10 * time.Second
)

type Console struct {
	IP         string    `json:"ip"`
	LastSeen   time.Time `json:"lastSeen"`
	Configured bool      `json:"configured"`
	Online     bool      `json:"online"`
}

type Snapshot struct {
	Listening    bool      `json:"listening"`
	Port         int       `json:"port"`
	ConfiguredIP string    `json:"configuredIp"`
	Consoles     []Console `json:"consoles"`
	Error        string    `json:"error,omitempty"`
}

type Listener struct {
	mu           sync.RWMutex
	port         int
	configuredIP string
	listening    bool
	lastError    string
	seen         map[string]time.Time
	now          func() time.Time
}

func New(configuredIP string) *Listener {
	return NewWithPort(configuredIP, BeaconPort)
}

func NewWithPort(configuredIP string, port int) *Listener {
	return &Listener{
		port:         port,
		configuredIP: strings.TrimSpace(configuredIP),
		seen:         make(map[string]time.Time),
		now:          time.Now,
	}
}

func (l *Listener) Listen(ctx context.Context) error {
	if l == nil {
		return errors.New("discovery listener is nil")
	}
	conn, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4zero, Port: l.port})
	if err != nil {
		l.setListenError(err)
		return err
	}
	defer conn.Close()

	actualPort := conn.LocalAddr().(*net.UDPAddr).Port
	l.mu.Lock()
	l.port = actualPort
	l.listening = true
	l.lastError = ""
	l.mu.Unlock()
	defer func() {
		l.mu.Lock()
		l.listening = false
		l.mu.Unlock()
	}()

	buf := make([]byte, 256)
	for {
		if err := ctx.Err(); err != nil {
			return nil
		}
		if err := conn.SetReadDeadline(time.Now().Add(750 * time.Millisecond)); err != nil {
			l.setListenError(err)
			return err
		}
		n, remote, err := conn.ReadFromUDP(buf)
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			var netErr net.Error
			if errors.As(err, &netErr) && netErr.Timeout() {
				continue
			}
			l.setListenError(err)
			return err
		}
		l.observe(remote.IP, buf[:n], l.now())
	}
}

func (l *Listener) Snapshot() Snapshot {
	if l == nil {
		return Snapshot{Port: BeaconPort}
	}
	now := l.now()
	l.mu.RLock()
	defer l.mu.RUnlock()

	consoles := make([]Console, 0, len(l.seen))
	for ip, lastSeen := range l.seen {
		consoles = append(consoles, Console{
			IP:         ip,
			LastSeen:   lastSeen,
			Configured: ip == l.configuredIP,
			Online:     now.Sub(lastSeen) <= onlineFor,
		})
	}
	sort.Slice(consoles, func(i, j int) bool {
		if consoles[i].Online != consoles[j].Online {
			return consoles[i].Online
		}
		if consoles[i].Configured != consoles[j].Configured {
			return consoles[i].Configured
		}
		return consoles[i].IP < consoles[j].IP
	})
	return Snapshot{
		Listening:    l.listening,
		Port:         l.port,
		ConfiguredIP: l.configuredIP,
		Consoles:     consoles,
		Error:        l.lastError,
	}
}

func (l *Listener) observe(ip net.IP, payload []byte, seenAt time.Time) bool {
	if l == nil || !bytes.HasPrefix(payload, []byte(BeaconMagic)) {
		return false
	}
	ip = ip.To4()
	if ip == nil || ip.IsLoopback() || ip.IsUnspecified() {
		return false
	}
	key := ip.String()
	l.mu.Lock()
	l.seen[key] = seenAt.UTC()
	l.mu.Unlock()
	return true
}

func (l *Listener) setListenError(err error) {
	l.mu.Lock()
	l.listening = false
	if err != nil {
		l.lastError = err.Error()
	}
	l.mu.Unlock()
}
