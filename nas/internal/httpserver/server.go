package httpserver

import (
	"bytes"
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
	"sync"
	"time"

	"github.com/Loopayeh/pkg-sender/nas/internal/discovery"
	"github.com/Loopayeh/pkg-sender/nas/internal/history"
	"github.com/Loopayeh/pkg-sender/nas/internal/pkgmeta"
	"github.com/Loopayeh/pkg-sender/nas/internal/pkgstore"
)

type Installer interface {
	Install(ctx context.Context, packageURL, name string) (string, error)
}

type DiscoveryProvider interface {
	Snapshot() discovery.Snapshot
}

type Server struct {
	store         *pkgstore.Store
	installer     Installer
	discovery     DiscoveryProvider
	history       *history.Store
	publicBaseURL string
	logger        *log.Logger
	transfers     *transferTracker
	queueMu       sync.Mutex
	queueRunning  bool
	queuePending  bool
	mux           *http.ServeMux
}

func New(store *pkgstore.Store, installer Installer, publicBaseURL string, logger *log.Logger, discoveryProvider ...DiscoveryProvider) (*Server, error) {
	return newServer(store, installer, publicBaseURL, logger, nil, discoveryProvider...)
}

func NewWithHistory(store *pkgstore.Store, installer Installer, publicBaseURL string, logger *log.Logger, historyStore *history.Store, discoveryProvider ...DiscoveryProvider) (*Server, error) {
	return newServer(store, installer, publicBaseURL, logger, historyStore, discoveryProvider...)
}

func newServer(store *pkgstore.Store, installer Installer, publicBaseURL string, logger *log.Logger, historyStore *history.Store, discoveryProvider ...DiscoveryProvider) (*Server, error) {
	if store == nil {
		return nil, errors.New("package store is required")
	}
	if installer == nil {
		return nil, errors.New("PS5 installer is required")
	}
	if len(discoveryProvider) > 1 {
		return nil, errors.New("only one discovery provider is supported")
	}
	if logger == nil {
		logger = log.New(os.Stdout, "", log.LstdFlags)
	}
	if historyStore == nil {
		historyStore = history.NewMemory(history.DefaultLimit)
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
		history:       historyStore,
		publicBaseURL: publicBaseURL,
		logger:        logger,
		transfers:     newTransferTracker(),
		mux:           http.NewServeMux(),
	}
	if len(discoveryProvider) == 1 {
		s.discovery = discoveryProvider[0]
	}
	s.routes()
	return s, nil
}

func (s *Server) Handler() http.Handler {
	return s.mux
}

func (s *Server) routes() {
	s.mux.HandleFunc("/", s.handleRoot)
	s.mux.HandleFunc("/ui", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/ui/", http.StatusPermanentRedirect)
	})
	s.mux.Handle("/ui/", http.StripPrefix("/ui/", newUIHandler()))
	s.mux.HandleFunc("/health", s.handleHealth)
	s.mux.HandleFunc("/api/packages", s.handlePackages)
	s.mux.HandleFunc("/api/families", s.handleFamilies)
	s.mux.HandleFunc("/api/transfers", s.handleTransfers)
	s.mux.HandleFunc("/api/history", s.handleHistory)
	s.mux.HandleFunc("/api/discovery", s.handleDiscovery)
	s.mux.HandleFunc("/api/rescan", s.handleRescan)
	s.mux.HandleFunc("/api/install/", s.handleInstall)
	s.mux.HandleFunc("/api/retry/", s.handleRetry)
	s.mux.HandleFunc("/api/reorder/", s.handleReorder)
	s.mux.HandleFunc("/api/cancel/", s.handleCancel)
	s.mux.HandleFunc("/icon/", s.handleIcon)
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
			"ui":        "GET /ui/",
			"packages":  "GET /api/packages",
			"families":  "GET /api/families",
			"transfers": "GET /api/transfers",
			"history":   "GET /api/history",
			"discovery": "GET /api/discovery",
			"rescan":    "POST /api/rescan",
			"install":   "POST /api/install/{id}",
			"retry":     "POST /api/retry/{historyId}",
			"reorder":   "POST /api/reorder/{historyId}",
			"cancel":    "POST /api/cancel/{historyId}",
			"icon":      "GET|HEAD /icon/{id}",
			"package":   "GET|HEAD /pkg/{id}",
			"health":    "GET /health",
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

func (s *Server) handleFamilies(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		methodNotAllowed(w, "GET, HEAD")
		return
	}
	writeJSON(w, http.StatusOK, s.store.Families())
}

func (s *Server) handleTransfers(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		methodNotAllowed(w, "GET, HEAD")
		return
	}
	writeJSON(w, http.StatusOK, s.transfers.List())
}

func (s *Server) handleHistory(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		methodNotAllowed(w, "GET, HEAD")
		return
	}
	writeJSON(w, http.StatusOK, s.history.List())
}

func (s *Server) handleDiscovery(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		methodNotAllowed(w, "GET, HEAD")
		return
	}
	if s.discovery == nil {
		writeJSON(w, http.StatusOK, discovery.Snapshot{
			Listening: false,
			Port:      discovery.BeaconPort,
			Error:     "discovery is not configured",
			Consoles:  []discovery.Console{},
		})
		return
	}
	snapshot := s.discovery.Snapshot()
	if snapshot.Consoles == nil {
		snapshot.Consoles = []discovery.Console{}
	}
	writeJSON(w, http.StatusOK, snapshot)
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

func (s *Server) handleIcon(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		methodNotAllowed(w, "GET, HEAD")
		return
	}

	id, ok := routeID(r.URL.Path, "/icon/")
	if !ok {
		http.NotFound(w, r)
		return
	}
	pkg, filePath, modTime, ok := s.store.Get(id)
	if !ok {
		http.NotFound(w, r)
		return
	}

	icon, err := pkgmeta.ReadIconFile(filePath)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("Cache-Control", "private, max-age=300")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	http.ServeContent(w, r, pkg.ID+".png", modTime, bytes.NewReader(icon))
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
	progress, tracked := s.transfers.Record(
		pkg.ID,
		r.Method,
		status,
		tw.Header().Get("Content-Range"),
		tw.bytes,
	)
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
	if tracked {
		if s.history != nil {
			record, updated, err := s.history.UpdateTransfer(
				pkg.ID,
				progress.Status,
				progress.Transferred,
				progress.Total,
				progress.RangeCount,
			)
			if err != nil {
				s.logger.Printf("persist transfer history for %s failed: %v", pkg.RelativePath, err)
			}
			if updated && record.QueueStatus == history.QueueComplete {
				s.kickQueue()
			}
		}
		s.logger.Printf(
			"transfer progress: id=%s status=%s transferred=%d total=%d percent=%.2f ranges=%d",
			progress.ID,
			progress.Status,
			progress.Transferred,
			progress.Total,
			progress.Percent,
			progress.RangeCount,
		)
	}
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
	if s.history.HasPendingPackage(pkg.ID) {
		writeError(w, http.StatusConflict, "package already has a queued or active install")
		return
	}

	packageURL := s.publicBaseURL + "/pkg/" + url.PathEscape(id)
	record, err := s.history.CreateQueued(pkg, packageURL, "")
	if err != nil {
		s.logger.Printf("enqueue install for %s failed: %v", pkg.RelativePath, err)
		if errors.Is(err, history.ErrUnavailable) {
			writeError(w, http.StatusServiceUnavailable, "install queue persistence is unavailable")
			return
		}
		writeError(w, http.StatusInternalServerError, "could not persist install queue")
		return
	}
	s.logger.Printf("install enqueued: history=%s file=%s", record.ID, pkg.RelativePath)
	s.kickQueue()
	writeJSON(w, http.StatusAccepted, map[string]any{
		"status":     "queued",
		"id":         pkg.ID,
		"historyId":  record.ID,
		"name":       pkg.Name,
		"packageUrl": packageURL,
	})
}

func (s *Server) handleRetry(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w, "POST")
		return
	}

	historyID, ok := routeID(r.URL.Path, "/api/retry/")
	if !ok {
		http.NotFound(w, r)
		return
	}
	previous, ok := s.history.Get(historyID)
	if !ok {
		http.NotFound(w, r)
		return
	}
	if !retryable(previous) {
		writeError(w, http.StatusConflict, "history record is not retryable")
		return
	}
	pkg, _, _, ok := s.store.Get(previous.PackageID)
	if !ok {
		writeError(w, http.StatusConflict, "package is no longer available")
		return
	}
	if s.history.HasPendingPackage(pkg.ID) {
		writeError(w, http.StatusConflict, "package already has a queued or active install")
		return
	}

	packageURL := s.publicBaseURL + "/pkg/" + url.PathEscape(pkg.ID)
	record, err := s.history.CreateQueued(pkg, packageURL, previous.ID)
	if err != nil {
		s.logger.Printf("enqueue retry for %s failed: %v", pkg.RelativePath, err)
		if errors.Is(err, history.ErrUnavailable) {
			writeError(w, http.StatusServiceUnavailable, "install queue persistence is unavailable")
			return
		}
		writeError(w, http.StatusInternalServerError, "could not persist install queue")
		return
	}
	s.logger.Printf("install retry enqueued: history=%s retry_of=%s file=%s", record.ID, previous.ID, pkg.RelativePath)
	s.kickQueue()
	writeJSON(w, http.StatusAccepted, map[string]any{
		"status":     "queued",
		"id":         pkg.ID,
		"historyId":  record.ID,
		"retryOf":    previous.ID,
		"name":       pkg.Name,
		"packageUrl": packageURL,
	})
}

func (s *Server) handleReorder(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w, "POST")
		return
	}

	historyID, ok := routeID(r.URL.Path, "/api/reorder/")
	if !ok {
		http.NotFound(w, r)
		return
	}
	record, ok := s.history.Get(historyID)
	if !ok {
		http.NotFound(w, r)
		return
	}
	if record.QueueStatus != history.QueueQueued {
		writeError(w, http.StatusConflict, "only queued installs can be reordered")
		return
	}

	var request struct {
		Direction string `json:"direction"`
	}
	decoder := json.NewDecoder(io.LimitReader(r.Body, 1024))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&request); err != nil {
		writeError(w, http.StatusBadRequest, "invalid reorder request")
		return
	}
	if request.Direction != "up" && request.Direction != "down" {
		writeError(w, http.StatusBadRequest, "direction must be up or down")
		return
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		writeError(w, http.StatusBadRequest, "invalid reorder request")
		return
	}

	moved, changed, err := s.history.MoveQueued(historyID, request.Direction)
	if err != nil {
		s.logger.Printf("reorder install queue history=%s direction=%s failed: %v", historyID, request.Direction, err)
		if errors.Is(err, history.ErrUnavailable) {
			writeError(w, http.StatusServiceUnavailable, "install queue persistence is unavailable")
			return
		}
		writeError(w, http.StatusInternalServerError, "could not persist queue reorder")
		return
	}
	if !changed {
		writeError(w, http.StatusConflict, "install cannot move further in that direction")
		return
	}
	s.logger.Printf("install queue reordered: history=%s direction=%s order=%d file=%s", moved.ID, request.Direction, moved.QueueOrder, moved.RelativePath)
	writeJSON(w, http.StatusOK, map[string]any{
		"status":     "reordered",
		"historyId":  moved.ID,
		"id":         moved.PackageID,
		"name":       moved.Name,
		"direction":  request.Direction,
		"queueOrder": moved.QueueOrder,
	})
}

func (s *Server) handleCancel(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w, "POST")
		return
	}

	historyID, ok := routeID(r.URL.Path, "/api/cancel/")
	if !ok {
		http.NotFound(w, r)
		return
	}
	record, ok := s.history.Get(historyID)
	if !ok {
		http.NotFound(w, r)
		return
	}
	if record.QueueStatus != history.QueueQueued {
		writeError(w, http.StatusConflict, "only queued installs can be cancelled")
		return
	}

	cancelled, changed, err := s.history.CancelQueued(historyID)
	if err != nil {
		s.logger.Printf("cancel install queue history=%s failed: %v", historyID, err)
		if errors.Is(err, history.ErrUnavailable) {
			writeError(w, http.StatusServiceUnavailable, "install queue persistence is unavailable")
			return
		}
		writeError(w, http.StatusInternalServerError, "could not persist queue cancellation")
		return
	}
	if !changed {
		writeError(w, http.StatusConflict, "install is no longer queued")
		return
	}
	s.logger.Printf("install queue cancelled: history=%s file=%s", cancelled.ID, cancelled.RelativePath)
	s.kickQueue()
	writeJSON(w, http.StatusOK, map[string]any{
		"status":    "cancelled",
		"historyId": cancelled.ID,
		"id":        cancelled.PackageID,
		"name":      cancelled.Name,
	})
}

func retryable(record history.Record) bool {
	if record.QueueStatus == history.QueueError || record.QueueStatus == history.QueueInterrupted {
		return true
	}
	if record.QueueStatus != "" {
		return false
	}
	return record.ControlStatus == "error" ||
		record.ControlStatus == "interrupted" ||
		record.TransferStatus == "interrupted"
}

func (s *Server) ResumeQueue() {
	s.kickQueue()
}

func (s *Server) kickQueue() {
	s.queueMu.Lock()
	s.queuePending = true
	if s.queueRunning {
		s.queueMu.Unlock()
		return
	}
	s.queueRunning = true
	s.queueMu.Unlock()
	go s.runQueue()
}

func (s *Server) runQueue() {
	for {
		s.queueMu.Lock()
		s.queuePending = false
		s.queueMu.Unlock()

		s.drainQueue()

		s.queueMu.Lock()
		if s.queuePending {
			s.queueMu.Unlock()
			continue
		}
		s.queueRunning = false
		s.queueMu.Unlock()
		return
	}
}

func (s *Server) drainQueue() {
	for {
		record, ok, err := s.history.ClaimNextQueued()
		if err != nil {
			s.logger.Printf("claim install queue failed: %v", err)
			return
		}
		if !ok {
			return
		}

		pkg, _, _, ok := s.store.Get(record.PackageID)
		if !ok {
			queueErr := errors.New("package is no longer available")
			if _, _, err := s.history.MarkQueueError(record.ID, queueErr); err != nil {
				s.logger.Printf("persist queue error for history=%s failed: %v", record.ID, err)
				return
			}
			s.logger.Printf("install queue skipped missing package: history=%s package=%s", record.ID, record.PackageID)
			continue
		}

		s.transfers.Start(pkg)
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		reply, installErr := s.installer.Install(ctx, record.PackageURL, pkg.Name)
		cancel()
		if installErr != nil {
			s.transfers.MarkError(pkg.ID, installErr)
			if _, _, err := s.history.MarkControlError(record.ID, installErr); err != nil {
				s.logger.Printf("persist install queue control error for history=%s failed: %v", record.ID, err)
				return
			}
			s.logger.Printf("install queue control request failed: history=%s file=%s error=%v receiver=%q", record.ID, pkg.RelativePath, installErr, reply)
			continue
		}

		s.transfers.MarkQueued(pkg.ID)
		updated, _, err := s.history.MarkAccepted(record.ID)
		if err != nil {
			s.logger.Printf("persist install queue acceptance for history=%s failed: %v", record.ID, err)
			return
		}
		s.logger.Printf("install queue submitted: history=%s file=%s receiver=%q", record.ID, pkg.RelativePath, reply)
		if updated.QueueStatus == history.QueueComplete {
			continue
		}
		return
	}
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
