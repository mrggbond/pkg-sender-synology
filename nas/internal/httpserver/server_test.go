package httpserver

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Loopayeh/pkg-sender/nas/internal/pkgstore"
)

type fakeInstaller struct {
	url  string
	name string
}

func (f *fakeInstaller) Install(_ context.Context, packageURL, name string) (string, error) {
	f.url = packageURL
	f.name = name
	return `{"status":"success"}`, nil
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
	if installResp.StatusCode != http.StatusOK {
		t.Fatalf("install status=%d, want 200", installResp.StatusCode)
	}

	wantURL := "http://192.168.1.20:9898/pkg/" + pkgs[0].ID
	if installer.url != wantURL {
		t.Fatalf("installer URL=%q, want %q", installer.url, wantURL)
	}
	if installer.name != "Game.pkg" {
		t.Fatalf("installer name=%q", installer.name)
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
	for _, want := range []string{"PS5 PKG Sender", "/api/families", "/api/transfers", "/api/install/", "/icon/", "pkg.contentId", "packageTypeLabel"} {
		if !bytes.Contains(body, []byte(want)) {
			t.Fatalf("UI does not contain %q", want)
		}
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
