package httpserver

import (
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/Loopayeh/pkg-sender/nas/internal/pkgstore"
)

type byteInterval struct {
	start int64
	end   int64
}

type TransferSnapshot struct {
	ID           string    `json:"id"`
	Name         string    `json:"name"`
	RelativePath string    `json:"relativePath"`
	Status       string    `json:"status"`
	Transferred  int64     `json:"transferred"`
	Total        int64     `json:"total"`
	Percent      float64   `json:"percent"`
	RangeCount   int       `json:"rangeCount"`
	StartedAt    time.Time `json:"startedAt"`
	UpdatedAt    time.Time `json:"updatedAt"`
	Error        string    `json:"error,omitempty"`
}

type transferSession struct {
	pkg         pkgstore.Package
	status      string
	intervals   []byteInterval
	transferred int64
	startedAt   time.Time
	updatedAt   time.Time
	err         string
}

type transferTracker struct {
	mu       sync.RWMutex
	sessions map[string]*transferSession
}

func newTransferTracker() *transferTracker {
	return &transferTracker{sessions: make(map[string]*transferSession)}
}

func (t *transferTracker) Start(pkg pkgstore.Package) TransferSnapshot {
	now := time.Now().UTC()
	t.mu.Lock()
	s := &transferSession{
		pkg:       pkg,
		status:    "requesting",
		startedAt: now,
		updatedAt: now,
	}
	t.sessions[pkg.ID] = s
	snapshot := transferSnapshot(s)
	t.mu.Unlock()
	return snapshot
}

func (t *transferTracker) MarkQueued(id string) (TransferSnapshot, bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	s, ok := t.sessions[id]
	if !ok {
		return TransferSnapshot{}, false
	}
	if s.status == "requesting" {
		s.status = "queued"
		s.updatedAt = time.Now().UTC()
	}
	return transferSnapshot(s), true
}

func (t *transferTracker) MarkError(id string, err error) (TransferSnapshot, bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	s, ok := t.sessions[id]
	if !ok {
		return TransferSnapshot{}, false
	}
	if s.status == "requesting" {
		s.status = "error"
		s.err = err.Error()
		s.updatedAt = time.Now().UTC()
	}
	return transferSnapshot(s), true
}

func (t *transferTracker) Record(id, method string, status int, contentRange string, bytesWritten int64) (TransferSnapshot, bool) {
	if method != "GET" || bytesWritten <= 0 || (status != 200 && status != 206) {
		return TransferSnapshot{}, false
	}

	t.mu.Lock()
	defer t.mu.Unlock()
	s, ok := t.sessions[id]
	if !ok || s.status == "error" || s.pkg.Size <= 0 {
		return TransferSnapshot{}, false
	}

	start, end, ok := transferredInterval(status, contentRange, bytesWritten, s.pkg.Size)
	if !ok {
		return TransferSnapshot{}, false
	}
	s.intervals = mergeIntervals(s.intervals, byteInterval{start: start, end: end})
	s.transferred = coveredBytes(s.intervals)
	s.updatedAt = time.Now().UTC()
	s.err = ""
	if s.transferred >= s.pkg.Size {
		s.transferred = s.pkg.Size
		s.status = "complete"
	} else {
		s.status = "downloading"
	}
	return transferSnapshot(s), true
}

func (t *transferTracker) List() []TransferSnapshot {
	t.mu.RLock()
	out := make([]TransferSnapshot, 0, len(t.sessions))
	for _, s := range t.sessions {
		out = append(out, transferSnapshot(s))
	}
	t.mu.RUnlock()

	sort.Slice(out, func(i, j int) bool {
		if out[i].StartedAt.Equal(out[j].StartedAt) {
			return out[i].ID < out[j].ID
		}
		return out[i].StartedAt.After(out[j].StartedAt)
	})
	return out
}

func transferSnapshot(s *transferSession) TransferSnapshot {
	percent := 0.0
	if s.pkg.Size > 0 {
		percent = float64(s.transferred) * 100 / float64(s.pkg.Size)
		if percent > 100 {
			percent = 100
		}
	}
	return TransferSnapshot{
		ID:           s.pkg.ID,
		Name:         s.pkg.Name,
		RelativePath: s.pkg.RelativePath,
		Status:       s.status,
		Transferred:  s.transferred,
		Total:        s.pkg.Size,
		Percent:      percent,
		RangeCount:   len(s.intervals),
		StartedAt:    s.startedAt,
		UpdatedAt:    s.updatedAt,
		Error:        s.err,
	}
}

func transferredInterval(status int, contentRange string, bytesWritten, total int64) (int64, int64, bool) {
	if bytesWritten <= 0 || total <= 0 {
		return 0, 0, false
	}
	if status == 200 {
		end := bytesWritten - 1
		if end >= total {
			end = total - 1
		}
		return 0, end, end >= 0
	}

	var start, declaredEnd, declaredTotal int64
	if _, err := fmt.Sscanf(strings.TrimSpace(contentRange), "bytes %d-%d/%d", &start, &declaredEnd, &declaredTotal); err != nil {
		return 0, 0, false
	}
	if start < 0 || declaredEnd < start || declaredTotal <= 0 || start >= total {
		return 0, 0, false
	}
	maxEnd := total - 1
	end := maxEnd
	if bytesWritten-1 <= maxEnd-start {
		end = start + bytesWritten - 1
	}
	if end > declaredEnd {
		end = declaredEnd
	}
	return start, end, end >= start
}

func mergeIntervals(existing []byteInterval, next byteInterval) []byteInterval {
	all := make([]byteInterval, 0, len(existing)+1)
	all = append(all, existing...)
	all = append(all, next)
	sort.Slice(all, func(i, j int) bool {
		if all[i].start == all[j].start {
			return all[i].end < all[j].end
		}
		return all[i].start < all[j].start
	})

	merged := make([]byteInterval, 0, len(all))
	for _, current := range all {
		if len(merged) == 0 {
			merged = append(merged, current)
			continue
		}
		last := &merged[len(merged)-1]
		if current.start <= last.end+1 {
			if current.end > last.end {
				last.end = current.end
			}
			continue
		}
		merged = append(merged, current)
	}
	return merged
}

func coveredBytes(intervals []byteInterval) int64 {
	var total int64
	for _, interval := range intervals {
		total += interval.end - interval.start + 1
	}
	return total
}
