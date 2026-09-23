package history

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/Loopayeh/pkg-sender/nas/internal/pkgstore"
)

const (
	fileVersion             = 1
	DefaultLimit            = 100
	progressPersistInterval = 5 * time.Second

	QueueQueued      = "queued"
	QueueSubmitting  = "submitting"
	QueueActive      = "active"
	QueueComplete    = "complete"
	QueueError       = "error"
	QueueInterrupted = "interrupted"
	QueueCancelled   = "cancelled"
)

var ErrUnavailable = errors.New("history store is unavailable")

type Record struct {
	ID             string    `json:"id"`
	PackageID      string    `json:"packageId"`
	Name           string    `json:"name"`
	RelativePath   string    `json:"relativePath"`
	Size           int64     `json:"size"`
	PackageURL     string    `json:"packageUrl"`
	QueueStatus    string    `json:"queueStatus,omitempty"`
	QueueOrder     int64     `json:"queueOrder,omitempty"`
	RetryOf        string    `json:"retryOf,omitempty"`
	QueueError     string    `json:"queueError,omitempty"`
	ControlStatus  string    `json:"controlStatus"`
	TransferStatus string    `json:"transferStatus"`
	InstallStatus  string    `json:"installStatus"`
	Transferred    int64     `json:"transferred"`
	Total          int64     `json:"total"`
	Percent        float64   `json:"percent"`
	RangeCount     int       `json:"rangeCount"`
	ControlError   string    `json:"controlError,omitempty"`
	StartedAt      time.Time `json:"startedAt"`
	UpdatedAt      time.Time `json:"updatedAt"`
}

type diskState struct {
	Version int      `json:"version"`
	Records []Record `json:"records"`
}

type Store struct {
	mu                  sync.RWMutex
	path                string
	limit               int
	records             []Record
	lastProgressPersist map[string]time.Time
	nextQueueOrder      int64
	now                 func() time.Time
	unavailable         error
}

func Open(path string, limit int) (*Store, error) {
	s := newStore(strings.TrimSpace(path), limit)
	if s.path == "" {
		return s, nil
	}
	data, err := os.ReadFile(s.path)
	if errors.Is(err, os.ErrNotExist) {
		return s, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read history file: %w", err)
	}
	var state diskState
	if err := json.Unmarshal(data, &state); err != nil {
		return nil, fmt.Errorf("decode history file: %w", err)
	}
	if state.Version != fileVersion {
		return nil, fmt.Errorf("unsupported history file version %d", state.Version)
	}
	s.records = append([]Record(nil), state.Records...)
	beforeNormalize := len(s.records)
	s.normalizeLocked()
	changed := len(s.records) != beforeNormalize
	if s.normalizeQueueOrderLocked() {
		changed = true
	}
	if s.interruptActiveLocked() {
		changed = true
	}
	if changed {
		if err := s.persistLocked(); err != nil {
			return nil, err
		}
	}
	return s, nil
}

func NewMemory(limit int) *Store {
	return newStore("", limit)
}

func NewUnavailable(cause error) *Store {
	if cause == nil {
		cause = ErrUnavailable
	} else {
		cause = fmt.Errorf("%w: %v", ErrUnavailable, cause)
	}
	s := newStore("", DefaultLimit)
	s.unavailable = cause
	return s
}

func newStore(path string, limit int) *Store {
	if limit <= 0 {
		limit = DefaultLimit
	}
	return &Store{
		path:                path,
		limit:               limit,
		lastProgressPersist: make(map[string]time.Time),
		nextQueueOrder:      1,
		now:                 time.Now,
	}
}

func (s *Store) Create(pkg pkgstore.Package, packageURL string) (Record, error) {
	if s == nil {
		return Record{}, errors.New("history store is nil")
	}
	if s.unavailable != nil {
		return Record{}, s.unavailable
	}
	id, err := newID()
	if err != nil {
		return Record{}, err
	}
	now := s.now().UTC()
	record := Record{
		ID:             id,
		PackageID:      pkg.ID,
		Name:           pkg.Name,
		RelativePath:   pkg.RelativePath,
		Size:           pkg.Size,
		PackageURL:     packageURL,
		ControlStatus:  "requesting",
		TransferStatus: "waiting",
		InstallStatus:  "unverified",
		Total:          pkg.Size,
		StartedAt:      now,
		UpdatedAt:      now,
	}

	s.mu.Lock()
	oldRecords := append([]Record(nil), s.records...)
	s.records = append([]Record{record}, s.records...)
	s.trimLocked()
	err = s.persistLocked()
	if err != nil {
		s.records = oldRecords
	}
	s.mu.Unlock()
	if err != nil {
		return Record{}, err
	}
	return record, nil
}

func (s *Store) CreateQueued(pkg pkgstore.Package, packageURL, retryOf string) (Record, error) {
	if s == nil {
		return Record{}, errors.New("history store is nil")
	}
	if s.unavailable != nil {
		return Record{}, s.unavailable
	}
	id, err := newID()
	if err != nil {
		return Record{}, err
	}
	now := s.now().UTC()
	record := Record{
		ID:             id,
		PackageID:      pkg.ID,
		Name:           pkg.Name,
		RelativePath:   pkg.RelativePath,
		Size:           pkg.Size,
		PackageURL:     packageURL,
		QueueStatus:    QueueQueued,
		RetryOf:        retryOf,
		ControlStatus:  "pending",
		TransferStatus: "not_started",
		InstallStatus:  "unverified",
		Total:          pkg.Size,
		StartedAt:      now,
		UpdatedAt:      now,
	}

	s.mu.Lock()
	oldRecords := append([]Record(nil), s.records...)
	oldNextQueueOrder := s.nextQueueOrder
	record.QueueOrder = s.nextQueueOrder
	s.nextQueueOrder++
	s.records = append([]Record{record}, s.records...)
	s.trimLocked()
	err = s.persistLocked()
	if err != nil {
		s.records = oldRecords
		s.nextQueueOrder = oldNextQueueOrder
	}
	s.mu.Unlock()
	if err != nil {
		return Record{}, err
	}
	return record, nil
}

func (s *Store) Get(id string) (Record, bool) {
	if s == nil {
		return Record{}, false
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	for i := range s.records {
		if s.records[i].ID == id {
			return s.records[i], true
		}
	}
	return Record{}, false
}

func (s *Store) HasPendingPackage(packageID string) bool {
	if s == nil {
		return false
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	for i := range s.records {
		if s.records[i].PackageID == packageID && isQueueBlocking(s.records[i].QueueStatus) {
			return true
		}
	}
	return false
}

func (s *Store) ClaimNextQueued() (Record, bool, error) {
	if s == nil {
		return Record{}, false, nil
	}
	if s.unavailable != nil {
		return Record{}, false, s.unavailable
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	for i := range s.records {
		if s.records[i].QueueStatus == QueueSubmitting || s.records[i].QueueStatus == QueueActive {
			return Record{}, false, nil
		}
	}

	index := -1
	for i := range s.records {
		if s.records[i].QueueStatus != QueueQueued {
			continue
		}
		if index < 0 || queueRecordBefore(s.records[i], s.records[index]) {
			index = i
		}
	}
	if index < 0 {
		return Record{}, false, nil
	}

	old := s.records[index]
	record := &s.records[index]
	record.QueueStatus = QueueSubmitting
	record.QueueError = ""
	record.ControlStatus = "requesting"
	record.ControlError = ""
	record.TransferStatus = "waiting"
	record.UpdatedAt = s.now().UTC()
	if err := s.persistLocked(); err != nil {
		s.records[index] = old
		return Record{}, false, err
	}
	return *record, true, nil
}

func (s *Store) CancelQueued(id string) (Record, bool, error) {
	if s == nil {
		return Record{}, false, nil
	}
	if s.unavailable != nil {
		return Record{}, false, s.unavailable
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.records {
		if s.records[i].ID != id {
			continue
		}
		if s.records[i].QueueStatus != QueueQueued {
			return s.records[i], false, nil
		}
		oldRecord := s.records[i]
		s.records[i].QueueStatus = QueueCancelled
		s.records[i].QueueError = ""
		s.records[i].UpdatedAt = s.now().UTC()
		if err := s.persistLocked(); err != nil {
			s.records[i] = oldRecord
			return oldRecord, false, err
		}
		return s.records[i], true, nil
	}
	return Record{}, false, nil
}

func (s *Store) MoveQueued(id, direction string) (Record, bool, error) {
	if s == nil {
		return Record{}, false, nil
	}
	if s.unavailable != nil {
		return Record{}, false, s.unavailable
	}
	if direction != "up" && direction != "down" {
		return Record{}, false, errors.New("queue direction must be up or down")
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	queued := make([]int, 0)
	target := -1
	for i := range s.records {
		if s.records[i].QueueStatus != QueueQueued {
			continue
		}
		queued = append(queued, i)
		if s.records[i].ID == id {
			target = i
		}
	}
	if target < 0 {
		for i := range s.records {
			if s.records[i].ID == id {
				return s.records[i], false, nil
			}
		}
		return Record{}, false, nil
	}
	sort.Slice(queued, func(i, j int) bool {
		return queueRecordBefore(s.records[queued[i]], s.records[queued[j]])
	})
	position := -1
	for i, index := range queued {
		if index == target {
			position = i
			break
		}
	}
	neighborPosition := position - 1
	if direction == "down" {
		neighborPosition = position + 1
	}
	if position < 0 || neighborPosition < 0 || neighborPosition >= len(queued) {
		return s.records[target], false, nil
	}

	neighbor := queued[neighborPosition]
	oldTarget := s.records[target]
	oldNeighbor := s.records[neighbor]
	s.records[target].QueueOrder, s.records[neighbor].QueueOrder =
		s.records[neighbor].QueueOrder, s.records[target].QueueOrder
	now := s.now().UTC()
	s.records[target].UpdatedAt = now
	s.records[neighbor].UpdatedAt = now
	if err := s.persistLocked(); err != nil {
		s.records[target] = oldTarget
		s.records[neighbor] = oldNeighbor
		return oldTarget, false, err
	}
	return s.records[target], true, nil
}

func (s *Store) MarkAccepted(id string) (Record, bool, error) {
	return s.update(id, func(record *Record) {
		if record.ControlStatus == "requesting" {
			record.ControlStatus = "accepted"
			record.UpdatedAt = s.now().UTC()
		}
		if record.QueueStatus == QueueSubmitting {
			if record.TransferStatus == "complete" {
				record.QueueStatus = QueueComplete
			} else {
				record.QueueStatus = QueueActive
			}
		}
	})
}

func (s *Store) MarkControlError(id string, controlErr error) (Record, bool, error) {
	return s.update(id, func(record *Record) {
		if record.QueueStatus == QueueSubmitting || record.QueueStatus == QueueActive {
			record.QueueStatus = QueueError
		}
		record.ControlStatus = "error"
		if controlErr != nil {
			record.ControlError = controlErr.Error()
		}
		if record.TransferStatus == "waiting" {
			record.TransferStatus = "not_started"
		}
		record.UpdatedAt = s.now().UTC()
	})
}

func (s *Store) MarkQueueError(id string, queueErr error) (Record, bool, error) {
	return s.update(id, func(record *Record) {
		record.QueueStatus = QueueError
		if queueErr != nil {
			record.QueueError = queueErr.Error()
		}
		if record.ControlStatus == "requesting" {
			record.ControlStatus = "pending"
		}
		if record.TransferStatus == "waiting" {
			record.TransferStatus = "not_started"
		}
		record.UpdatedAt = s.now().UTC()
	})
}

func (s *Store) UpdateTransfer(packageID, status string, transferred, total int64, rangeCount int) (Record, bool, error) {
	if s == nil {
		return Record{}, false, nil
	}
	if s.unavailable != nil {
		return Record{}, false, s.unavailable
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	index := -1
	for i := range s.records {
		if s.records[i].PackageID != packageID {
			continue
		}
		switch s.records[i].TransferStatus {
		case "complete", "interrupted", "not_started":
			continue
		}
		index = i
		break
	}
	if index < 0 {
		return Record{}, false, nil
	}

	oldRecord := s.records[index]
	record := &s.records[index]
	switch status {
	case "downloading":
		record.TransferStatus = "downloading"
	case "complete":
		record.TransferStatus = "complete"
		if record.QueueStatus == QueueSubmitting || record.QueueStatus == QueueActive {
			record.QueueStatus = QueueComplete
		}
	default:
		return *record, false, nil
	}
	record.Transferred = transferred
	record.Total = total
	record.RangeCount = rangeCount
	if total > 0 {
		record.Percent = float64(transferred) * 100 / float64(total)
		if record.Percent > 100 {
			record.Percent = 100
		}
	}
	now := s.now().UTC()
	record.UpdatedAt = now

	lastPersist := s.lastProgressPersist[record.ID]
	shouldPersist := status == "complete" || lastPersist.IsZero() || now.Sub(lastPersist) >= progressPersistInterval
	if !shouldPersist {
		return *record, true, nil
	}
	err := s.persistLocked()
	if err != nil {
		s.records[index] = oldRecord
		return oldRecord, true, err
	}
	s.lastProgressPersist[record.ID] = now
	return *record, true, nil
}

func (s *Store) List() []Record {
	if s == nil {
		return []Record{}
	}
	s.mu.RLock()
	out := make([]Record, len(s.records))
	copy(out, s.records)
	s.mu.RUnlock()
	return out
}

func (s *Store) update(id string, mutate func(*Record)) (Record, bool, error) {
	if s == nil {
		return Record{}, false, nil
	}
	if s.unavailable != nil {
		return Record{}, false, s.unavailable
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.records {
		if s.records[i].ID != id {
			continue
		}
		oldRecord := s.records[i]
		mutate(&s.records[i])
		if err := s.persistLocked(); err != nil {
			s.records[i] = oldRecord
			return oldRecord, true, err
		}
		return s.records[i], true, nil
	}
	return Record{}, false, nil
}

func (s *Store) interruptActiveLocked() bool {
	changed := false
	now := s.now().UTC()
	for i := range s.records {
		record := &s.records[i]
		recordChanged := false
		if record.QueueStatus == QueueSubmitting || record.QueueStatus == QueueActive {
			record.QueueStatus = QueueInterrupted
			recordChanged = true
		}
		if record.ControlStatus == "requesting" {
			record.ControlStatus = "interrupted"
			recordChanged = true
		}
		switch record.TransferStatus {
		case "waiting", "downloading":
			record.TransferStatus = "interrupted"
			recordChanged = true
		}
		if recordChanged {
			record.UpdatedAt = now
			changed = true
		}
	}
	return changed
}

func (s *Store) normalizeLocked() {
	sort.SliceStable(s.records, func(i, j int) bool {
		if s.records[i].StartedAt.Equal(s.records[j].StartedAt) {
			return s.records[i].ID < s.records[j].ID
		}
		return s.records[i].StartedAt.After(s.records[j].StartedAt)
	})
	s.trimLocked()
}

func (s *Store) normalizeQueueOrderLocked() bool {
	queued := make([]int, 0)
	allRanked := true
	for i := range s.records {
		if s.records[i].QueueStatus != QueueQueued {
			continue
		}
		queued = append(queued, i)
		if s.records[i].QueueOrder <= 0 {
			allRanked = false
		}
	}
	if len(queued) == 0 {
		s.nextQueueOrder = 1
		return false
	}
	if allRanked {
		sort.Slice(queued, func(i, j int) bool {
			return queueRecordBefore(s.records[queued[i]], s.records[queued[j]])
		})
	} else {
		sort.Slice(queued, func(i, j int) bool {
			a, b := s.records[queued[i]], s.records[queued[j]]
			if a.StartedAt.Equal(b.StartedAt) {
				return a.ID < b.ID
			}
			return a.StartedAt.Before(b.StartedAt)
		})
	}
	changed := false
	for position, index := range queued {
		order := int64(position + 1)
		if s.records[index].QueueOrder != order {
			s.records[index].QueueOrder = order
			changed = true
		}
	}
	s.nextQueueOrder = int64(len(queued) + 1)
	return changed
}

func queueRecordBefore(a, b Record) bool {
	if a.QueueOrder > 0 && b.QueueOrder > 0 && a.QueueOrder != b.QueueOrder {
		return a.QueueOrder < b.QueueOrder
	}
	if a.StartedAt.Equal(b.StartedAt) {
		return a.ID < b.ID
	}
	return a.StartedAt.Before(b.StartedAt)
}

func (s *Store) trimLocked() {
	if len(s.records) <= s.limit {
		return
	}
	trimmed := make([]Record, 0, s.limit)
	for i := range s.records {
		if len(trimmed) < s.limit || isQueueBlocking(s.records[i].QueueStatus) {
			trimmed = append(trimmed, s.records[i])
		}
	}
	s.records = trimmed
}

func isQueueBlocking(status string) bool {
	return status == QueueQueued || status == QueueSubmitting || status == QueueActive
}

func (s *Store) persistLocked() error {
	if s.path == "" {
		return nil
	}
	dir := filepath.Dir(s.path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("create history directory: %w", err)
	}
	state := diskState{Version: fileVersion, Records: s.records}
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return fmt.Errorf("encode history: %w", err)
	}
	data = append(data, '\n')

	tmp, err := os.CreateTemp(dir, ".history-*.tmp")
	if err != nil {
		return fmt.Errorf("create history temp file: %w", err)
	}
	tmpName := tmp.Name()
	cleanup := func() {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
	}
	if err := tmp.Chmod(0o600); err != nil {
		cleanup()
		return fmt.Errorf("chmod history temp file: %w", err)
	}
	if _, err := tmp.Write(data); err != nil {
		cleanup()
		return fmt.Errorf("write history temp file: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		cleanup()
		return fmt.Errorf("sync history temp file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpName)
		return fmt.Errorf("close history temp file: %w", err)
	}
	if err := os.Rename(tmpName, s.path); err != nil {
		_ = os.Remove(tmpName)
		return fmt.Errorf("replace history file: %w", err)
	}
	if dirFile, err := os.Open(dir); err == nil {
		_ = dirFile.Sync()
		_ = dirFile.Close()
	}
	return nil
}

func newID() (string, error) {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", fmt.Errorf("generate history id: %w", err)
	}
	return hex.EncodeToString(raw[:]), nil
}
