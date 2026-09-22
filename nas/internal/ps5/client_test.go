package ps5

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestInstallUsesReceiverCompatibleEncodedURL(t *testing.T) {
	var got installRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/install" {
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Fatal(err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"success"}`))
	}))
	defer srv.Close()

	client, err := NewWithBaseURL(srv.URL, &http.Client{Timeout: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	rawURL := "http://192.168.1.20:9898/pkg/abc"
	if _, err := client.Install(context.Background(), rawURL, "Game.pkg"); err != nil {
		t.Fatal(err)
	}

	if got.Type != "direct" || got.Name != "Game.pkg" || len(got.Packages) != 1 {
		t.Fatalf("unexpected payload: %#v", got)
	}
	want := "http%3A%2F%2F192.168.1.20%3A9898%2Fpkg%2Fabc"
	if got.Packages[0] != want {
		t.Fatalf("package URL=%q, want %q", got.Packages[0], want)
	}
}

func TestInstallRejectsReceiverFailure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"status":"fail","error":"queue failed"}`))
	}))
	defer srv.Close()

	client, err := NewWithBaseURL(srv.URL, &http.Client{Timeout: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.Install(context.Background(), "http://nas:9898/pkg/id", "x.pkg"); err == nil {
		t.Fatal("expected receiver failure")
	}
}
