package httpserver

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Loopayeh/pkg-sender/nas/internal/discovery"
	"github.com/Loopayeh/pkg-sender/nas/internal/history"
	"github.com/Loopayeh/pkg-sender/nas/internal/pkgstore"
)

type fakeInstaller struct {
	mu    sync.Mutex
	calls []installCall
}

type installCall struct {
	url  string
	name string
}

func (f *fakeInstaller) Install(_ context.Context, packageURL, name string) (string, error) {
	f.mu.Lock()
	f.calls = append(f.calls, installCall{url: packageURL, name: name})
	f.mu.Unlock()
	return `{"status":"success"}`, nil
}

func (f *fakeInstaller) Calls() []installCall {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]installCall, len(f.calls))
	copy(out, f.calls)
	return out
}

type failingInstaller struct {
	err error
}

func (f failingInstaller) Install(_ context.Context, _, _ string) (string, error) {
	return `{"status":"error"}`, f.err
}

type fakeDiscovery struct {
	snapshot discovery.Snapshot
}

func (f fakeDiscovery) Snapshot() discovery.Snapshot {
	return f.snapshot
}

func waitFor(t *testing.T, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if condition() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("timed out waiting for asynchronous condition")
}

func TestPackageRangeAndInstallFlow(t *testing.T) {
	root := t.TempDir()
	pkgPath := filepath.Join(root, "nested", "Game.pkg")
	if err := os.MkdirAll(filepath.Dir(pkgPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(pkgPath, []byte("0123456789"), 0o644); err != nil {
		t.Fatal(err)
	}

	store, err := pkgstore.New(root)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Scan(); err != nil {
		t.Fatal(err)
	}
	pkgs := store.List()
	if len(pkgs) != 1 {
		t.Fatalf("packages=%d, want 1", len(pkgs))
	}

	installer := &fakeInstaller{}
	app, err := New(store, installer, "http://192.168.1.20:9898", log.New(io.Discard, "", 0))
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(app.Handler())
	defer srv.Close()

	req, err := http.NewRequest(http.MethodGet, srv.URL+"/pkg/"+pkgs[0].ID, nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Range", "bytes=2-5")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()

	if resp.StatusCode != http.StatusPartialContent {
		t.Fatalf("range status=%d, want 206", resp.StatusCode)
	}
	if string(body) != "2345" {
		t.Fatalf("range body=%q, want 2345", string(body))
	}
	if got := resp.Header.Get("Content-Range"); got != "bytes 2-5/10" {
		t.Fatalf("Content-Range=%q", got)
	}
	if got := resp.Header.Get("Accept-Ranges"); got != "bytes" {
		t.Fatalf("Accept-Ranges=%q, want bytes", got)
	}

	badReq, err := http.NewRequest(http.MethodGet, srv.URL+"/pkg/"+pkgs[0].ID, nil)
	if err != nil {
		t.Fatal(err)
	}
	badReq.Header.Set("Range", "bytes=100-")
	badResp, err := http.DefaultClient.Do(badReq)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = io.Copy(io.Discard, badResp.Body)
	badResp.Body.Close()
	if badResp.StatusCode != http.StatusRequestedRangeNotSatisfiable {
		t.Fatalf("invalid range status=%d, want 416", badResp.StatusCode)
	}
	if got := badResp.Header.Get("Content-Range"); got != "bytes */10" {
		t.Fatalf("invalid range Content-Range=%q", got)
	}

	headResp, err := http.Head(srv.URL + "/pkg/" + pkgs[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	headResp.Body.Close()
	if headResp.StatusCode != http.StatusOK || headResp.ContentLength != 10 {
		t.Fatalf("HEAD status=%d length=%d", headResp.StatusCode, headResp.ContentLength)
	}

	installReq, err := http.NewRequest(http.MethodPost, srv.URL+"/api/install/"+pkgs[0].ID, nil)
	if err != nil {
		t.Fatal(err)
	}
	installResp, err := http.DefaultClient.Do(installReq)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = io.Copy(io.Discard, installResp.Body)
	installResp.Body.Close()
	if installResp.StatusCode != http.StatusAccepted {
		t.Fatalf("install status=%d, want 202", installResp.StatusCode)
	}

	wantURL := "http://192.168.1.20:9898/pkg/" + pkgs[0].ID
	waitFor(t, func() bool { return len(installer.Calls()) == 1 })
	call := installer.Calls()[0]
	if call.url != wantURL {
		t.Fatalf("installer URL=%q, want %q", call.url, wantURL)
	}
	if call.name != "Game.pkg" {
		t.Fatalf("installer name=%q", call.name)
	}

	progressReq, err := http.NewRequest(http.MethodGet, srv.URL+"/pkg/"+pkgs[0].ID, nil)
	if err != nil {
		t.Fatal(err)
	}
	progressReq.Header.Set("Range", "bytes=0-4")
	progressResp, err := http.DefaultClient.Do(progressReq)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = io.Copy(io.Discard, progressResp.Body)
	progressResp.Body.Close()
	if progressResp.StatusCode != http.StatusPartialContent {
		t.Fatalf("progress range status=%d", progressResp.StatusCode)
	}

	transfersResp, err := http.Get(srv.URL + "/api/transfers")
	if err != nil {
		t.Fatal(err)
	}
	var transfers []TransferSnapshot
	if err := json.NewDecoder(transfersResp.Body).Decode(&transfers); err != nil {
		transfersResp.Body.Close()
		t.Fatal(err)
	}
	transfersResp.Body.Close()
	if len(transfers) != 1 {
		t.Fatalf("transfers=%d, want 1", len(transfers))
	}
	if transfers[0].ID != pkgs[0].ID || transfers[0].Transferred != 5 || transfers[0].Total != 10 || transfers[0].Status != "downloading" {
		t.Fatalf("transfer snapshot=%+v", transfers[0])
	}
}

func TestEmbeddedWebUI(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "Game.pkg"), []byte("0123456789"), 0o644); err != nil {
		t.Fatal(err)
	}
	store, err := pkgstore.New(root)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Scan(); err != nil {
		t.Fatal(err)
	}
	app, err := New(store, &fakeInstaller{}, "http://192.168.1.20:9898", log.New(io.Discard, "", 0))
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(app.Handler())
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/ui/")
	if err != nil {
		t.Fatal(err)
	}
	body, err := io.ReadAll(resp.Body)
	resp.Body.Close()
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("UI status=%d, want 200", resp.StatusCode)
	}
	if got := resp.Header.Get("Content-Type"); !strings.HasPrefix(got, "text/html") {
		t.Fatalf("UI Content-Type=%q", got)
	}
	for _, want := range []string{"PS5 PKG Sender", "/api/families", "/api/transfers", "/api/history", "/api/discovery", "/api/install/", "/api/retry/", "/api/cancel/", "/api/reorder/", "/icon/", "queueStatus", "queueOrder", "Retry", "Cancel", "cancelled", "Up", "Down", "pkg.contentId", "packageTypeLabel", "Recent activity", "Install outcome unverified"} {
		if !bytes.Contains(body, []byte(want)) {
			t.Fatalf("UI does not contain %q", want)
		}
	}
}

func TestDiscoveryAPI(t *testing.T) {
	root := t.TempDir()
	store, err := pkgstore.New(root)
	if err != nil {
		t.Fatal(err)
	}
	lastSeen := time.Date(2026, 9, 23, 4, 30, 0, 0, time.UTC)
	provider := fakeDiscovery{snapshot: discovery.Snapshot{
		Listening:    true,
		Port:         discovery.BeaconPort,
		ConfiguredIP: "192.168.32.100",
		Consoles: []discovery.Console{{
			IP:         "192.168.32.100",
			LastSeen:   lastSeen,
			Configured: true,
			Online:     true,
		}},
	}}
	app, err := New(store, &fakeInstaller{}, "http://192.168.32.5:9898", log.New(io.Discard, "", 0), provider)
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(app.Handler())
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/api/discovery")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("discovery status=%d, want 200", resp.StatusCode)
	}
	var got discovery.Snapshot
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	if !got.Listening || got.Port != discovery.BeaconPort || got.ConfiguredIP != "192.168.32.100" {
		t.Fatalf("discovery snapshot=%+v", got)
	}
	if len(got.Consoles) != 1 || !got.Consoles[0].Configured || !got.Consoles[0].Online || !got.Consoles[0].LastSeen.Equal(lastSeen) {
		t.Fatalf("discovery consoles=%+v", got.Consoles)
	}
}

func TestInstallHistoryTracksAcceptedTransferToCompletion(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "Game.pkg"), []byte("0123456789"), 0o644); err != nil {
		t.Fatal(err)
	}
	store, err := pkgstore.New(root)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Scan(); err != nil {
		t.Fatal(err)
	}
	pkg := store.List()[0]
	historyStore := history.NewMemory(10)
	app, err := NewWithHistory(store, &fakeInstaller{}, "http://192.168.1.20:9898", log.New(io.Discard, "", 0), historyStore)
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(app.Handler())
	defer srv.Close()

	installReq, _ := http.NewRequest(http.MethodPost, srv.URL+"/api/install/"+pkg.ID, nil)
	installResp, err := http.DefaultClient.Do(installReq)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = io.Copy(io.Discard, installResp.Body)
	installResp.Body.Close()
	if installResp.StatusCode != http.StatusAccepted {
		t.Fatalf("install status=%d, want 202", installResp.StatusCode)
	}
	waitFor(t, func() bool {
		records := historyStore.List()
		return len(records) == 1 &&
			records[0].QueueStatus == history.QueueActive &&
			records[0].ControlStatus == "accepted"
	})

	duplicateReq, _ := http.NewRequest(http.MethodPost, srv.URL+"/api/install/"+pkg.ID, nil)
	duplicateResp, err := http.DefaultClient.Do(duplicateReq)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = io.Copy(io.Discard, duplicateResp.Body)
	duplicateResp.Body.Close()
	if duplicateResp.StatusCode != http.StatusConflict {
		t.Fatalf("duplicate pending install status=%d, want 409", duplicateResp.StatusCode)
	}

	rangeReq, _ := http.NewRequest(http.MethodGet, srv.URL+"/pkg/"+pkg.ID, nil)
	rangeReq.Header.Set("Range", "bytes=0-9")
	rangeResp, err := http.DefaultClient.Do(rangeReq)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = io.Copy(io.Discard, rangeResp.Body)
	rangeResp.Body.Close()
	if rangeResp.StatusCode != http.StatusPartialContent {
		t.Fatalf("range status=%d, want 206", rangeResp.StatusCode)
	}
	waitFor(t, func() bool {
		records := historyStore.List()
		return len(records) == 1 && records[0].QueueStatus == history.QueueComplete
	})

	historyResp, err := http.Get(srv.URL + "/api/history")
	if err != nil {
		t.Fatal(err)
	}
	var records []history.Record
	if err := json.NewDecoder(historyResp.Body).Decode(&records); err != nil {
		historyResp.Body.Close()
		t.Fatal(err)
	}
	historyResp.Body.Close()
	if len(records) != 1 {
		t.Fatalf("history records=%d, want 1", len(records))
	}
	got := records[0]
	if got.PackageID != pkg.ID || got.QueueStatus != history.QueueComplete || got.ControlStatus != "accepted" || got.TransferStatus != "complete" || got.InstallStatus != "unverified" {
		t.Fatalf("history record=%+v", got)
	}
	if got.Transferred != 10 || got.Total != 10 || got.Percent != 100 || got.RangeCount != 1 {
		t.Fatalf("history progress=%+v", got)
	}

	secondReq, _ := http.NewRequest(http.MethodPost, srv.URL+"/api/install/"+pkg.ID, nil)
	secondResp, err := http.DefaultClient.Do(secondReq)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = io.Copy(io.Discard, secondResp.Body)
	secondResp.Body.Close()
	if secondResp.StatusCode != http.StatusAccepted {
		t.Fatalf("second install status=%d, want 202", secondResp.StatusCode)
	}
	waitFor(t, func() bool {
		records = historyStore.List()
		return len(records) == 2 &&
			records[0].ID != records[1].ID &&
			records[0].QueueStatus == history.QueueActive
	})
	if records[0].ControlStatus != "accepted" || records[0].TransferStatus != "waiting" {
		t.Fatalf("newest history record=%+v", records[0])
	}
}

func TestInstallHistoryRecordsReceiverControlFailure(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "Game.pkg"), []byte("0123456789"), 0o644); err != nil {
		t.Fatal(err)
	}
	store, err := pkgstore.New(root)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Scan(); err != nil {
		t.Fatal(err)
	}
	pkg := store.List()[0]
	historyStore := history.NewMemory(10)
	app, err := NewWithHistory(
		store,
		failingInstaller{err: errors.New("receiver unavailable")},
		"http://192.168.1.20:9898",
		log.New(io.Discard, "", 0),
		historyStore,
	)
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(app.Handler())
	defer srv.Close()

	req, _ := http.NewRequest(http.MethodPost, srv.URL+"/api/install/"+pkg.ID, nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = io.Copy(io.Discard, resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("install status=%d, want 202", resp.StatusCode)
	}

	var records []history.Record
	waitFor(t, func() bool {
		records = historyStore.List()
		return len(records) == 1 && records[0].QueueStatus == history.QueueError
	})
	got := records[0]
	if got.QueueStatus != history.QueueError || got.ControlStatus != "error" || got.TransferStatus != "not_started" || got.InstallStatus != "unverified" {
		t.Fatalf("history record=%+v", got)
	}
	if got.ControlError != "receiver unavailable" {
		t.Fatalf("control error=%q", got.ControlError)
	}

	retryReq, _ := http.NewRequest(http.MethodPost, srv.URL+"/api/retry/"+got.ID, nil)
	retryResp, err := http.DefaultClient.Do(retryReq)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = io.Copy(io.Discard, retryResp.Body)
	retryResp.Body.Close()
	if retryResp.StatusCode != http.StatusAccepted {
		t.Fatalf("retry status=%d, want 202", retryResp.StatusCode)
	}
	waitFor(t, func() bool {
		records = historyStore.List()
		return len(records) == 2 &&
			records[0].RetryOf == got.ID &&
			records[0].QueueStatus == history.QueueError
	})
	if records[1].ID != got.ID {
		t.Fatalf("retry overwrote original history: %+v", records)
	}
}

func TestInstallQueueWaitsForTransferCompletionBeforeSubmittingNext(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{"A.pkg", "B.pkg"} {
		if err := os.WriteFile(filepath.Join(root, name), []byte("0123456789"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	store, err := pkgstore.New(root)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Scan(); err != nil {
		t.Fatal(err)
	}
	var first, second pkgstore.Package
	for _, pkg := range store.List() {
		switch pkg.Name {
		case "A.pkg":
			first = pkg
		case "B.pkg":
			second = pkg
		}
	}
	if first.ID == "" || second.ID == "" {
		t.Fatalf("missing packages: first=%+v second=%+v", first, second)
	}

	installer := &fakeInstaller{}
	historyStore := history.NewMemory(10)
	app, err := NewWithHistory(store, installer, "http://192.168.1.20:9898", log.New(io.Discard, "", 0), historyStore)
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(app.Handler())
	defer srv.Close()

	for _, pkg := range []pkgstore.Package{first, second} {
		req, _ := http.NewRequest(http.MethodPost, srv.URL+"/api/install/"+pkg.ID, nil)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		_, _ = io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
		if resp.StatusCode != http.StatusAccepted {
			t.Fatalf("enqueue %s status=%d, want 202", pkg.Name, resp.StatusCode)
		}
	}

	waitFor(t, func() bool {
		records := historyStore.List()
		if len(records) != 2 || len(installer.Calls()) != 1 {
			return false
		}
		var firstStatus, secondStatus string
		for _, record := range records {
			switch record.PackageID {
			case first.ID:
				firstStatus = record.QueueStatus
			case second.ID:
				secondStatus = record.QueueStatus
			}
		}
		return firstStatus == history.QueueActive && secondStatus == history.QueueQueued
	})
	if calls := installer.Calls(); len(calls) != 1 || calls[0].name != first.Name {
		t.Fatalf("installer calls before first transfer completes=%+v", calls)
	}

	rangeReq, _ := http.NewRequest(http.MethodGet, srv.URL+"/pkg/"+first.ID, nil)
	rangeReq.Header.Set("Range", "bytes=0-9")
	rangeResp, err := http.DefaultClient.Do(rangeReq)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = io.Copy(io.Discard, rangeResp.Body)
	rangeResp.Body.Close()
	if rangeResp.StatusCode != http.StatusPartialContent {
		t.Fatalf("first full range status=%d, want 206", rangeResp.StatusCode)
	}

	waitFor(t, func() bool { return len(installer.Calls()) == 2 })
	calls := installer.Calls()
	if calls[1].name != second.Name {
		t.Fatalf("second submitted package=%q, want %q", calls[1].name, second.Name)
	}
	waitFor(t, func() bool {
		records := historyStore.List()
		for _, record := range records {
			if record.PackageID == second.ID {
				return record.QueueStatus == history.QueueActive
			}
		}
		return false
	})
}

func TestCancelOnlyQueuedInstallAndDoNotContactReceiver(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{"A.pkg", "B.pkg"} {
		if err := os.WriteFile(filepath.Join(root, name), []byte("0123456789"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	store, err := pkgstore.New(root)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Scan(); err != nil {
		t.Fatal(err)
	}
	var first, second pkgstore.Package
	for _, pkg := range store.List() {
		switch pkg.Name {
		case "A.pkg":
			first = pkg
		case "B.pkg":
			second = pkg
		}
	}
	if first.ID == "" || second.ID == "" {
		t.Fatalf("missing packages: first=%+v second=%+v", first, second)
	}

	installer := &fakeInstaller{}
	historyStore := history.NewMemory(10)
	app, err := NewWithHistory(store, installer, "http://192.168.1.20:9898", log.New(io.Discard, "", 0), historyStore)
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(app.Handler())
	defer srv.Close()

	for _, pkg := range []pkgstore.Package{first, second} {
		req, _ := http.NewRequest(http.MethodPost, srv.URL+"/api/install/"+pkg.ID, nil)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		_, _ = io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
		if resp.StatusCode != http.StatusAccepted {
			t.Fatalf("enqueue %s status=%d, want 202", pkg.Name, resp.StatusCode)
		}
	}

	var firstHistoryID, secondHistoryID string
	waitFor(t, func() bool {
		if len(installer.Calls()) != 1 {
			return false
		}
		for _, record := range historyStore.List() {
			switch record.PackageID {
			case first.ID:
				if record.QueueStatus == history.QueueActive {
					firstHistoryID = record.ID
				}
			case second.ID:
				if record.QueueStatus == history.QueueQueued {
					secondHistoryID = record.ID
				}
			}
		}
		return firstHistoryID != "" && secondHistoryID != ""
	})

	cancelReq, _ := http.NewRequest(http.MethodPost, srv.URL+"/api/cancel/"+secondHistoryID, nil)
	cancelResp, err := http.DefaultClient.Do(cancelReq)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = io.Copy(io.Discard, cancelResp.Body)
	cancelResp.Body.Close()
	if cancelResp.StatusCode != http.StatusOK {
		t.Fatalf("cancel queued status=%d, want 200", cancelResp.StatusCode)
	}
	cancelled, ok := historyStore.Get(secondHistoryID)
	if !ok || cancelled.QueueStatus != history.QueueCancelled {
		t.Fatalf("cancelled record=%+v ok=%v", cancelled, ok)
	}
	time.Sleep(25 * time.Millisecond)
	if calls := installer.Calls(); len(calls) != 1 {
		t.Fatalf("cancelling queued item contacted receiver: %+v", calls)
	}

	activeCancelReq, _ := http.NewRequest(http.MethodPost, srv.URL+"/api/cancel/"+firstHistoryID, nil)
	activeCancelResp, err := http.DefaultClient.Do(activeCancelReq)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = io.Copy(io.Discard, activeCancelResp.Body)
	activeCancelResp.Body.Close()
	if activeCancelResp.StatusCode != http.StatusConflict {
		t.Fatalf("cancel active status=%d, want 409", activeCancelResp.StatusCode)
	}
	active, _ := historyStore.Get(firstHistoryID)
	if active.QueueStatus != history.QueueActive {
		t.Fatalf("active record changed by cancel: %+v", active)
	}

	requeueReq, _ := http.NewRequest(http.MethodPost, srv.URL+"/api/install/"+second.ID, nil)
	requeueResp, err := http.DefaultClient.Do(requeueReq)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = io.Copy(io.Discard, requeueResp.Body)
	requeueResp.Body.Close()
	if requeueResp.StatusCode != http.StatusAccepted {
		t.Fatalf("requeue cancelled package status=%d, want 202", requeueResp.StatusCode)
	}
	time.Sleep(25 * time.Millisecond)
	if calls := installer.Calls(); len(calls) != 1 {
		t.Fatalf("requeued item bypassed active FIFO slot: %+v", calls)
	}
}

func TestReorderQueuedChangesNextReceiverSubmission(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{"A.pkg", "B.pkg", "C.pkg"} {
		if err := os.WriteFile(filepath.Join(root, name), []byte("0123456789"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	store, err := pkgstore.New(root)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Scan(); err != nil {
		t.Fatal(err)
	}
	byName := map[string]pkgstore.Package{}
	for _, pkg := range store.List() {
		byName[pkg.Name] = pkg
	}
	first, second, third := byName["A.pkg"], byName["B.pkg"], byName["C.pkg"]
	if first.ID == "" || second.ID == "" || third.ID == "" {
		t.Fatalf("missing packages: %+v", byName)
	}

	installer := &fakeInstaller{}
	historyStore := history.NewMemory(10)
	app, err := NewWithHistory(store, installer, "http://192.168.1.20:9898", log.New(io.Discard, "", 0), historyStore)
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(app.Handler())
	defer srv.Close()

	for _, pkg := range []pkgstore.Package{first, second, third} {
		req, _ := http.NewRequest(http.MethodPost, srv.URL+"/api/install/"+pkg.ID, nil)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		_, _ = io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
		if resp.StatusCode != http.StatusAccepted {
			t.Fatalf("enqueue %s status=%d, want 202", pkg.Name, resp.StatusCode)
		}
	}

	var firstHistoryID, secondHistoryID, thirdHistoryID string
	waitFor(t, func() bool {
		if len(installer.Calls()) != 1 {
			return false
		}
		for _, record := range historyStore.List() {
			switch record.PackageID {
			case first.ID:
				if record.QueueStatus == history.QueueActive {
					firstHistoryID = record.ID
				}
			case second.ID:
				if record.QueueStatus == history.QueueQueued {
					secondHistoryID = record.ID
				}
			case third.ID:
				if record.QueueStatus == history.QueueQueued {
					thirdHistoryID = record.ID
				}
			}
		}
		return firstHistoryID != "" && secondHistoryID != "" && thirdHistoryID != ""
	})

	activeReqBody := strings.NewReader(`{"direction":"down"}`)
	activeReq, _ := http.NewRequest(http.MethodPost, srv.URL+"/api/reorder/"+firstHistoryID, activeReqBody)
	activeReq.Header.Set("Content-Type", "application/json")
	activeResp, err := http.DefaultClient.Do(activeReq)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = io.Copy(io.Discard, activeResp.Body)
	activeResp.Body.Close()
	if activeResp.StatusCode != http.StatusConflict {
		t.Fatalf("reorder active status=%d, want 409", activeResp.StatusCode)
	}

	invalidReq, _ := http.NewRequest(http.MethodPost, srv.URL+"/api/reorder/"+thirdHistoryID, strings.NewReader(`{"direction":"left"}`))
	invalidReq.Header.Set("Content-Type", "application/json")
	invalidResp, err := http.DefaultClient.Do(invalidReq)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = io.Copy(io.Discard, invalidResp.Body)
	invalidResp.Body.Close()
	if invalidResp.StatusCode != http.StatusBadRequest {
		t.Fatalf("invalid reorder status=%d, want 400", invalidResp.StatusCode)
	}

	reorderReq, _ := http.NewRequest(http.MethodPost, srv.URL+"/api/reorder/"+thirdHistoryID, strings.NewReader(`{"direction":"up"}`))
	reorderReq.Header.Set("Content-Type", "application/json")
	reorderResp, err := http.DefaultClient.Do(reorderReq)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = io.Copy(io.Discard, reorderResp.Body)
	reorderResp.Body.Close()
	if reorderResp.StatusCode != http.StatusOK {
		t.Fatalf("reorder queued status=%d, want 200", reorderResp.StatusCode)
	}
	secondRecord, _ := historyStore.Get(secondHistoryID)
	thirdRecord, _ := historyStore.Get(thirdHistoryID)
	if thirdRecord.QueueOrder >= secondRecord.QueueOrder {
		t.Fatalf("reorder did not place C before B: B=%+v C=%+v", secondRecord, thirdRecord)
	}
	if calls := installer.Calls(); len(calls) != 1 {
		t.Fatalf("reorder contacted receiver before active transfer completed: %+v", calls)
	}

	rangeReq, _ := http.NewRequest(http.MethodGet, srv.URL+"/pkg/"+first.ID, nil)
	rangeReq.Header.Set("Range", "bytes=0-9")
	rangeResp, err := http.DefaultClient.Do(rangeReq)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = io.Copy(io.Discard, rangeResp.Body)
	rangeResp.Body.Close()
	if rangeResp.StatusCode != http.StatusPartialContent {
		t.Fatalf("first full range status=%d, want 206", rangeResp.StatusCode)
	}

	waitFor(t, func() bool { return len(installer.Calls()) == 2 })
	calls := installer.Calls()
	if calls[1].name != third.Name {
		t.Fatalf("second receiver submission=%q, want reordered %q", calls[1].name, third.Name)
	}
}

func TestResumeQueueOnlySubmitsPersistedQueuedRecords(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{"Interrupted.pkg", "Queued.pkg"} {
		if err := os.WriteFile(filepath.Join(root, name), []byte("0123456789"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	store, err := pkgstore.New(root)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Scan(); err != nil {
		t.Fatal(err)
	}
	var interruptedPkg, queuedPkg pkgstore.Package
	for _, pkg := range store.List() {
		switch pkg.Name {
		case "Interrupted.pkg":
			interruptedPkg = pkg
		case "Queued.pkg":
			queuedPkg = pkg
		}
	}
	if interruptedPkg.ID == "" || queuedPkg.ID == "" {
		t.Fatalf("missing packages: interrupted=%+v queued=%+v", interruptedPkg, queuedPkg)
	}

	historyPath := filepath.Join(t.TempDir(), "history.json")
	historyStore, err := history.Open(historyPath, 10)
	if err != nil {
		t.Fatal(err)
	}
	interrupted, err := historyStore.CreateQueued(interruptedPkg, "http://192.168.1.20:9898/pkg/"+interruptedPkg.ID, "")
	if err != nil {
		t.Fatal(err)
	}
	claimed, ok, err := historyStore.ClaimNextQueued()
	if err != nil || !ok || claimed.ID != interrupted.ID {
		t.Fatalf("claim interrupted candidate=%+v ok=%v err=%v", claimed, ok, err)
	}
	if _, ok, err := historyStore.MarkAccepted(interrupted.ID); err != nil || !ok {
		t.Fatalf("MarkAccepted ok=%v err=%v", ok, err)
	}
	queued, err := historyStore.CreateQueued(queuedPkg, "http://192.168.1.20:9898/pkg/"+queuedPkg.ID, "")
	if err != nil {
		t.Fatal(err)
	}

	reloaded, err := history.Open(historyPath, 10)
	if err != nil {
		t.Fatal(err)
	}
	gotInterrupted, ok := reloaded.Get(interrupted.ID)
	if !ok || gotInterrupted.QueueStatus != history.QueueInterrupted {
		t.Fatalf("active record was not interrupted on reload: %+v ok=%v", gotInterrupted, ok)
	}
	gotQueued, ok := reloaded.Get(queued.ID)
	if !ok || gotQueued.QueueStatus != history.QueueQueued {
		t.Fatalf("queued record did not survive reload: %+v ok=%v", gotQueued, ok)
	}

	installer := &fakeInstaller{}
	app, err := NewWithHistory(store, installer, "http://192.168.1.20:9898", log.New(io.Discard, "", 0), reloaded)
	if err != nil {
		t.Fatal(err)
	}
	app.ResumeQueue()
	waitFor(t, func() bool { return len(installer.Calls()) == 1 })
	calls := installer.Calls()
	if calls[0].name != queuedPkg.Name {
		t.Fatalf("resume submitted=%q, want queued package %q", calls[0].name, queuedPkg.Name)
	}
	gotInterrupted, _ = reloaded.Get(interrupted.ID)
	if gotInterrupted.QueueStatus != history.QueueInterrupted {
		t.Fatalf("resume replayed interrupted record: %+v", gotInterrupted)
	}
	gotQueued, _ = reloaded.Get(queued.ID)
	if gotQueued.QueueStatus != history.QueueActive {
		t.Fatalf("queued record did not become active: %+v", gotQueued)
	}
}

func TestUnavailablePersistentQueueFailsClosed(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "Game.pkg"), []byte("0123456789"), 0o644); err != nil {
		t.Fatal(err)
	}
	store, err := pkgstore.New(root)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Scan(); err != nil {
		t.Fatal(err)
	}
	pkg := store.List()[0]
	installer := &fakeInstaller{}
	app, err := NewWithHistory(
		store,
		installer,
		"http://192.168.1.20:9898",
		log.New(io.Discard, "", 0),
		history.NewUnavailable(errors.New("corrupt history")),
	)
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(app.Handler())
	defer srv.Close()

	healthResp, err := http.Get(srv.URL + "/health")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = io.Copy(io.Discard, healthResp.Body)
	healthResp.Body.Close()
	if healthResp.StatusCode != http.StatusOK {
		t.Fatalf("health status=%d, want 200", healthResp.StatusCode)
	}

	req, _ := http.NewRequest(http.MethodPost, srv.URL+"/api/install/"+pkg.ID, nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = io.Copy(io.Discard, resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("install status=%d, want 503", resp.StatusCode)
	}
	time.Sleep(25 * time.Millisecond)
	if calls := installer.Calls(); len(calls) != 0 {
		t.Fatalf("unavailable queue contacted receiver: %+v", calls)
	}
}

func TestFamiliesAPIKeepsUnknownPackagesVisible(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "Game.pkg"), []byte("not-a-real-pkg"), 0o644); err != nil {
		t.Fatal(err)
	}
	store, err := pkgstore.New(root)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Scan(); err != nil {
		t.Fatal(err)
	}
	app, err := New(store, &fakeInstaller{}, "http://192.168.1.20:9898", log.New(io.Discard, "", 0))
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(app.Handler())
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/api/families")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("families status=%d, want 200", resp.StatusCode)
	}
	var families []pkgstore.Family
	if err := json.NewDecoder(resp.Body).Decode(&families); err != nil {
		t.Fatal(err)
	}
	if len(families) != 1 || families[0].PackageCount != 1 || len(families[0].Packages) != 1 {
		t.Fatalf("unexpected fallback families: %+v", families)
	}
	if families[0].Packages[0].Name != "Game.pkg" {
		t.Fatalf("fallback package disappeared: %+v", families[0])
	}
}

func TestIconEndpointServesPNGForGETAndHEAD(t *testing.T) {
	root := t.TempDir()
	png := append([]byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'}, []byte("fixture-icon")...)
	pkgPath := filepath.Join(root, "Game.pkg")
	if err := os.WriteFile(pkgPath, buildIconPackageFixture(png), 0o644); err != nil {
		t.Fatal(err)
	}

	store, err := pkgstore.New(root)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Scan(); err != nil {
		t.Fatal(err)
	}
	packages := store.List()
	if len(packages) != 1 {
		t.Fatalf("packages=%d, want 1", len(packages))
	}

	app, err := New(store, &fakeInstaller{}, "http://192.168.1.20:9898", log.New(io.Discard, "", 0))
	if err != nil {
		t.Fatal(err)
	}

	getReq := httptest.NewRequest(http.MethodGet, "/icon/"+packages[0].ID, nil)
	getRec := httptest.NewRecorder()
	app.Handler().ServeHTTP(getRec, getReq)
	if getRec.Code != http.StatusOK {
		t.Fatalf("GET icon status=%d, want 200", getRec.Code)
	}
	if got := getRec.Header().Get("Content-Type"); got != "image/png" {
		t.Fatalf("GET icon Content-Type=%q", got)
	}
	if got := getRec.Header().Get("X-Content-Type-Options"); got != "nosniff" {
		t.Fatalf("GET icon X-Content-Type-Options=%q", got)
	}
	if !bytes.Equal(getRec.Body.Bytes(), png) {
		t.Fatalf("GET icon body=%x, want %x", getRec.Body.Bytes(), png)
	}

	headReq := httptest.NewRequest(http.MethodHead, "/icon/"+packages[0].ID, nil)
	headRec := httptest.NewRecorder()
	app.Handler().ServeHTTP(headRec, headReq)
	if headRec.Code != http.StatusOK {
		t.Fatalf("HEAD icon status=%d, want 200", headRec.Code)
	}
	if headRec.Body.Len() != 0 {
		t.Fatalf("HEAD icon body len=%d, want 0", headRec.Body.Len())
	}
	if got := headRec.Header().Get("Content-Type"); got != "image/png" {
		t.Fatalf("HEAD icon Content-Type=%q", got)
	}
}

func TestIconEndpointReturnsNotFoundWhenPackageHasNoPNGIcon(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "Game.pkg"), []byte("not-a-real-pkg"), 0o644); err != nil {
		t.Fatal(err)
	}
	store, err := pkgstore.New(root)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Scan(); err != nil {
		t.Fatal(err)
	}
	pkg := store.List()[0]
	app, err := New(store, &fakeInstaller{}, "http://192.168.1.20:9898", log.New(io.Discard, "", 0))
	if err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodGet, "/icon/"+pkg.ID, nil)
	rec := httptest.NewRecorder()
	app.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("icon status=%d, want 404", rec.Code)
	}
}

func buildIconPackageFixture(png []byte) []byte {
	const (
		tableOff = 0x600
		dataOff  = 0x800
	)
	out := make([]byte, dataOff+len(png)+0x20)
	copy(out[:4], []byte{0x7f, 'C', 'N', 'T'})
	binary.BigEndian.PutUint32(out[0x10:0x14], 1)
	binary.BigEndian.PutUint32(out[0x18:0x1c], tableOff)
	copy(out[0x40:0x70], []byte("UP0001-PPSA12345_00-ICONFIXTURE000001"))

	binary.BigEndian.PutUint32(out[tableOff:tableOff+4], 0x1200)
	binary.BigEndian.PutUint32(out[tableOff+0x10:tableOff+0x14], dataOff)
	binary.BigEndian.PutUint32(out[tableOff+0x14:tableOff+0x18], uint32(len(png)))
	copy(out[dataOff:], png)

	return out
}

func TestLargePackageRangeUses64BitOffsets(t *testing.T) {
	root := t.TempDir()
	pkgPath := filepath.Join(root, "Large.pkg")
	f, err := os.Create(pkgPath)
	if err != nil {
		t.Fatal(err)
	}
	const size int64 = (1 << 32) + 32
	if err := f.Truncate(size); err != nil {
		_ = f.Close()
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}

	store, err := pkgstore.New(root)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Scan(); err != nil {
		t.Fatal(err)
	}
	pkg := store.List()[0]

	app, err := New(store, &fakeInstaller{}, "http://192.168.1.20:9898", log.New(io.Discard, "", 0))
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(app.Handler())
	defer srv.Close()

	req, err := http.NewRequest(http.MethodGet, srv.URL+"/pkg/"+pkg.ID, nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Range", "bytes=4294967296-4294967299")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()

	if resp.StatusCode != http.StatusPartialContent {
		t.Fatalf("status=%d, want 206", resp.StatusCode)
	}
	if got := resp.Header.Get("Content-Range"); got != "bytes 4294967296-4294967299/4294967328" {
		t.Fatalf("Content-Range=%q", got)
	}
	if len(body) != 4 {
		t.Fatalf("body len=%d, want 4", len(body))
	}
}

func TestPackageTransferLogCapturesRangeStatusAndBytes(t *testing.T) {
	root := t.TempDir()
	pkgPath := filepath.Join(root, "Game.pkg")
	if err := os.WriteFile(pkgPath, []byte("0123456789"), 0o644); err != nil {
		t.Fatal(err)
	}

	store, err := pkgstore.New(root)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Scan(); err != nil {
		t.Fatal(err)
	}
	pkg := store.List()[0]

	var logs bytes.Buffer
	app, err := New(store, &fakeInstaller{}, "http://192.168.1.20:9898", log.New(&logs, "", 0))
	if err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodGet, "/pkg/"+pkg.ID, nil)
	req.Header.Set("Range", "bytes=2-5")
	rec := httptest.NewRecorder()
	app.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusPartialContent {
		t.Fatalf("status=%d, want 206", rec.Code)
	}
	if got := rec.Body.String(); got != "2345" {
		t.Fatalf("body=%q, want 2345", got)
	}

	gotLog := logs.String()
	for _, want := range []string{
		"pkg transfer:",
		"method=GET",
		"client=192.0.2.1",
		"file=\"Game.pkg\"",
		"range=\"bytes=2-5\"",
		"status=206",
		"bytes=4",
	} {
		if !strings.Contains(gotLog, want) {
			t.Fatalf("log %q does not contain %q", gotLog, want)
		}
	}
}

func TestRescanAddsNewPackage(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "A.pkg"), []byte("a"), 0o644); err != nil {
		t.Fatal(err)
	}
	store, err := pkgstore.New(root)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Scan(); err != nil {
		t.Fatal(err)
	}

	app, err := New(store, &fakeInstaller{}, "http://192.168.1.20:9898", log.New(io.Discard, "", 0))
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(app.Handler())
	defer srv.Close()

	if err := os.WriteFile(filepath.Join(root, "B.pkg"), []byte("b"), 0o644); err != nil {
		t.Fatal(err)
	}
	req, _ := http.NewRequest(http.MethodPost, srv.URL+"/api/rescan", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = io.Copy(io.Discard, resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("rescan status=%d", resp.StatusCode)
	}
	if len(store.List()) != 2 {
		t.Fatalf("package count=%d, want 2", len(store.List()))
	}
}
