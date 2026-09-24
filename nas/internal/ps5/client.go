package ps5

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

type Client struct {
	baseURL string
	http    *http.Client
}

type Target struct {
	IP   string `json:"ip"`
	Port int    `json:"port"`
}

type DynamicClient struct {
	mu      sync.RWMutex
	ip      string
	port    int
	timeout time.Duration
}

type installRequest struct {
	Type     string   `json:"type"`
	Packages []string `json:"packages"`
	Name     string   `json:"name,omitempty"`
}

type installReply struct {
	Status string `json:"status"`
	Error  string `json:"error"`
}

func New(ip string, port int, timeout time.Duration) (*Client, error) {
	parsed, err := normalizeIP(ip)
	if err != nil {
		return nil, err
	}
	if port < 1 || port > 65535 {
		return nil, errors.New("PS5 port must be between 1 and 65535")
	}
	hostPort := net.JoinHostPort(parsed, strconv.Itoa(port))
	return NewWithBaseURL("http://"+hostPort, &http.Client{Timeout: timeout})
}

func NewWithBaseURL(baseURL string, httpClient *http.Client) (*Client, error) {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	u, err := url.Parse(baseURL)
	if err != nil || u.Scheme != "http" || u.Host == "" {
		return nil, errors.New("PS5 base URL must be a valid http URL")
	}
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 10 * time.Second}
	}
	return &Client{baseURL: baseURL, http: httpClient}, nil
}

func NewDynamic(ip string, port int, timeout time.Duration) (*DynamicClient, error) {
	parsed := ""
	if strings.TrimSpace(ip) != "" {
		var err error
		parsed, err = normalizeIP(ip)
		if err != nil {
			return nil, err
		}
	}
	if port < 1 || port > 65535 {
		return nil, errors.New("PS5 port must be between 1 and 65535")
	}
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	return &DynamicClient{ip: parsed, port: port, timeout: timeout}, nil
}

func normalizeIP(ip string) (string, error) {
	parsed := net.ParseIP(strings.TrimSpace(ip))
	if parsed == nil {
		return "", errors.New("PS5 IP must be a literal IPv4 or IPv6 address")
	}
	return parsed.String(), nil
}

func (c *DynamicClient) Target() Target {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return Target{IP: c.ip, Port: c.port}
}

func (c *DynamicClient) SetIP(ip string) error {
	parsed, err := normalizeIP(ip)
	if err != nil {
		return err
	}
	c.mu.Lock()
	c.ip = parsed
	c.mu.Unlock()
	return nil
}

func (c *DynamicClient) Install(ctx context.Context, packageURL, name string) (string, error) {
	c.mu.RLock()
	ip, port, timeout := c.ip, c.port, c.timeout
	c.mu.RUnlock()
	if strings.TrimSpace(ip) == "" {
		return "", errors.New("PS5 IP is not configured")
	}
	client, err := New(ip, port, timeout)
	if err != nil {
		return "", err
	}
	return client.Install(ctx, packageURL, name)
}

func (c *Client) Install(ctx context.Context, packageURL, name string) (string, error) {
	if strings.TrimSpace(packageURL) == "" {
		return "", errors.New("package URL is required")
	}

	// pkg-receiver's json_first_package() percent-decodes packages[0].
	// Keep parity with the upstream C# client, which sends an encoded URL.
	encodedURL := strings.ReplaceAll(url.QueryEscape(packageURL), "+", "%20")
	payload, err := json.Marshal(installRequest{
		Type:     "direct",
		Packages: []string{encodedURL},
		Name:     name,
	})
	if err != nil {
		return "", err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/api/install", bytes.NewReader(payload))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return "", fmt.Errorf("PS5 receiver request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
	if err != nil {
		return "", fmt.Errorf("read PS5 receiver reply: %w", err)
	}
	raw := strings.TrimSpace(string(body))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return raw, fmt.Errorf("PS5 receiver returned HTTP %d: %s", resp.StatusCode, raw)
	}

	var reply installReply
	if err := json.Unmarshal(body, &reply); err != nil {
		return raw, fmt.Errorf("invalid PS5 receiver JSON reply: %w", err)
	}
	if !strings.EqualFold(reply.Status, "success") {
		if reply.Error == "" {
			reply.Error = raw
		}
		return raw, fmt.Errorf("PS5 receiver rejected install: %s", reply.Error)
	}
	return raw, nil
}
