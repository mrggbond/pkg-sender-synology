package httpserver

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"path"
	"strings"

	"github.com/Loopayeh/pkg-sender/nas/internal/pkgstore"
)

type Installer interface {
	Install(ctx context.Context, packageURL, name string) (string, error)
}

type Server struct {
	store         *pkgstore.Store
	installer     Installer
	publicBaseURL string
	logger        *log.Logger
	mux           *http.ServeMux
}

func New(store *pkgstore.Store, installer Installer, publicBaseURL string, logger *log.Logger) (*Server, error) {
	if store == nil {
		return nil, errors.New("package store is required")
	}
	if installer == nil {
		return nil, errors.New("PS5 installer is required")
	}
	if logger == nil {
		logger = log.New(os.Stdout, "", log.LstdFlags)
	}

	publicBaseURL = strings.TrimRight(strings.TrimSpace(publicBaseURL), "/")
	if publicBaseURL == "" {
		return nil, errors.New("PKGSENDER_PUBLIC_BASE_URL is required")
	}
	u, err := url.Parse(publicBaseURL)
	if err != nil || u.Scheme != "http" || u.Host == "" {
		return nil, errors.New("PKGSENDER_PUBLIC_BASE_URL must be a valid http URL")
	}
	if u.RawQuery != "" || u.Fragment != "" {
		return nil, errors.New("PKGSENDER_PUBLIC_BASE_URL must not contain query or fragment")
	}
	if u.Path != "" && u.Path != "/" {
		return nil, errors.New("PKGSENDER_PUBLIC_BASE_URL must not contain a path")
	}
	publicBaseURL = strings.TrimRight(u.String(), "/")

	s := &Server{
		store:         store,
		installer:     installer,
		publicBaseURL: publicBaseURL,
		logger:        logger,
		mux:           http.NewServeMux(),
	}
	s.routes()
	return s, nil
}

func (s *Server) Handler() http.Handler {
	return s.mux
}

func (s *Server) routes() {
	s.mux.HandleFunc("/", s.handleRoot)
	s.mux.HandleFunc("/health", s.handleHealth)
	s.mux.HandleFunc("/api/packages", s.handlePackages)
	s.mux.HandleFunc("/api/rescan", s.handleRescan)
	s.mux.HandleFunc("/api/install/", s.handleInstall)
	s.mux.HandleFunc("/pkg/", s.handlePackage)
}

func (s *Server) handleRoot(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		methodNotAllowed(w, "GET, HEAD")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"service":  "pkg-sender-nas",
		"packages": len(s.store.List()),
		"endpoints": map[string]string{
			"packages": "GET /api/packages",
			"rescan":   "POST /api/rescan",
			"install":  "POST /api/install/{id}",
			"package":  "GET|HEAD /pkg/{id}",
			"health":   "GET /health",
		},
	})
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		methodNotAllowed(w, "GET, HEAD")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"status":   "ok",
		"packages": len(s.store.List()),
	})
}

func (s *Server) handlePackages(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		methodNotAllowed(w, "GET, HEAD")
		return
	}
	writeJSON(w, http.StatusOK, s.store.List())
}

func (s *Server) handleRescan(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w, "POST")
		return
	}
	count, err := s.store.Scan()
	if err != nil {
		s.logger.Printf("rescan failed: %v", err)
		writeError(w, http.StatusInternalServerError, "rescan failed")
		return
	}
	s.logger.Printf("library rescanned: %d pkg file(s)", count)
	writeJSON(w, http.StatusOK, map[string]any{"packages": count})
}

func (s *Server) handlePackage(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		methodNotAllowed(w, "GET, HEAD")
		return
	}

	id, ok := routeID(r.URL.Path, "/pkg/")
	if !ok {
		http.NotFound(w, r)
		return
	}
	pkg, filePath, modTime, ok := s.store.Get(id)
	if !ok {
		http.NotFound(w, r)
		return
	}

	f, err := os.Open(filePath)
	if err != nil {
		s.logger.Printf("open package %s failed: %v", pkg.RelativePath, err)
		http.NotFound(w, r)
		return
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() {
		http.NotFound(w, r)
		return
	}

	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	tw := &transferResponseWriter{ResponseWriter: w}
	http.ServeContent(tw, r, pkg.Name, modTime, f)

	status := tw.status
	if status == 0 {
		status = http.StatusOK
	}
	s.logger.Printf(
		"pkg transfer: method=%s client=%s id=%s file=%q range=%q status=%d bytes=%d",
		r.Method,
		clientIP(r.RemoteAddr),
		pkg.ID,
		pkg.RelativePath,
		r.Header.Get("Range"),
		status,
		tw.bytes,
	)
}

func (s *Server) handleInstall(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w, "POST")
		return
	}

	id, ok := routeID(r.URL.Path, "/api/install/")
	if !ok {
		http.NotFound(w, r)
		return
	}
	pkg, _, _, ok := s.store.Get(id)
	if !ok {
		http.NotFound(w, r)
		return
	}

	packageURL := s.publicBaseURL + "/pkg/" + url.PathEscape(id)

	reply, err := s.installer.Install(r.Context(), packageURL, pkg.Name)
	if err != nil {
		s.logger.Printf("install request failed for %s: %v", pkg.RelativePath, err)
		writeJSON(w, http.StatusBadGateway, map[string]any{
			"status":   "error",
			"error":    err.Error(),
			"receiver": reply,
		})
		return
	}

	s.logger.Printf("install queued: %s -> %s", pkg.RelativePath, packageURL)
	writeJSON(w, http.StatusOK, map[string]any{
		"status":     "queued",
		"id":         pkg.ID,
		"name":       pkg.Name,
		"packageUrl": packageURL,
		"receiver":   reply,
	})
}

func routeID(requestPath, prefix string) (string, bool) {
	raw := strings.TrimPrefix(requestPath, prefix)
	if raw == requestPath || raw == "" || strings.Contains(raw, "/") {
		return "", false
	}
	id, err := url.PathUnescape(raw)
	if err != nil || id == "" || path.Base(id) != id {
		return "", false
	}
	return id, true
}

type transferResponseWriter struct {
	http.ResponseWriter
	status int
	bytes  int64
}

func (w *transferResponseWriter) WriteHeader(status int) {
	if w.status != 0 {
		return
	}
	w.status = status
	w.ResponseWriter.WriteHeader(status)
}

func (w *transferResponseWriter) Write(p []byte) (int, error) {
	if w.status == 0 {
		w.status = http.StatusOK
	}
	n, err := w.ResponseWriter.Write(p)
	w.bytes += int64(n)
	return n, err
}

func (w *transferResponseWriter) ReadFrom(r io.Reader) (int64, error) {
	if w.status == 0 {
		w.status = http.StatusOK
	}
	if readerFrom, ok := w.ResponseWriter.(io.ReaderFrom); ok {
		n, err := readerFrom.ReadFrom(r)
		w.bytes += n
		return n, err
	}
	n, err := io.Copy(w.ResponseWriter, r)
	w.bytes += n
	return n, err
}

func (w *transferResponseWriter) Unwrap() http.ResponseWriter {
	return w.ResponseWriter
}

func clientIP(remoteAddr string) string {
	host, _, err := net.SplitHostPort(remoteAddr)
	if err == nil {
		return host
	}
	return remoteAddr
}

func methodNotAllowed(w http.ResponseWriter, allow string) {
	w.Header().Set("Allow", allow)
	writeError(w, http.StatusMethodNotAllowed, "method not allowed")
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSONStatus(w, status, map[string]string{"error": message})
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	writeJSONStatus(w, status, value)
}

func writeJSONStatus(w http.ResponseWriter, status int, value any) {
	data, err := json.Marshal(value)
	if err != nil {
		http.Error(w, fmt.Sprintf("json encode: %v", err), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_, _ = w.Write(append(data, '\n'))
}
