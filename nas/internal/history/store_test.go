package history

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Loopayeh/pkg-sender/nas/internal/pkgstore"
)

func TestHistoryEmptyListIsNonNil(t *testing.T) {
	records := NewMemory(10).List()
	if records == nil {
		t.Fatal("empty history must serialize as [] rather than null")
	}
	if len(records) != 0 {
		t.Fatalf("records=%d, want 0", len(records))
	}
}

func TestHistoryPersistsAndReloadsTerminalRecord(t *testing.T) {
	path := filepath.Join(t.TempDir(), "history.json")
	store, err := Open(path, 10)
	if err != nil {
		t.Fatal(err)
	}
	pkg := pkgstore.Package{ID: "pkg-1", Name: "Game.pkg", RelativePath: "Games/Game.pkg", Size: 100}
	record, err := store.Create(pkg, "http://nas:9898/pkg/pkg-1")
	if err != nil {
		t.Fatal(err)
	}
	if _, ok, err := store.MarkAccepted(record.ID); err != nil || !ok {
		t.Fatalf("MarkAccepted ok=%v err=%v", ok, err)
	}
	if _, ok, err := store.UpdateTransfer(pkg.ID, "complete", 100, 100, 1); err != nil || !ok {
		t.Fatalf("UpdateTransfer ok=%v err=%v", ok, err)
	}

	reloaded, err := Open(path, 10)
	if err != nil {
		t.Fatal(err)
	}
	records := reloaded.List()
	if len(records) != 1 {
		t.Fatalf("records=%d, want 1", len(records))
	}
	got := records[0]
	if got.ID != record.ID || got.ControlStatus != "accepted" || got.TransferStatus != "complete" || got.InstallStatus != "unverified" || got.Transferred != 100 {
		t.Fatalf("reloaded record=%+v", got)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("history mode=%#o, want 0600", info.Mode().Perm())
	}
}

func TestHistoryMarksActiveRecordInterruptedOnReload(t *testing.T) {
	path := filepath.Join(t.TempDir(), "history.json")
	store, err := Open(path, 10)
	if err != nil {
		t.Fatal(err)
	}
	pkg := pkgstore.Package{ID: "pkg-2", Name: "Game.pkg", RelativePath: "Game.pkg", Size: 100}
	record, err := store.Create(pkg, "http://nas/pkg/pkg-2")
	if err != nil {
		t.Fatal(err)
	}
	if _, ok, err := store.MarkAccepted(record.ID); err != nil || !ok {
		t.Fatalf("MarkAccepted ok=%v err=%v", ok, err)
	}
	if _, ok, err := store.UpdateTransfer(pkg.ID, "downloading", 40, 100, 2); err != nil || !ok {
		t.Fatalf("UpdateTransfer ok=%v err=%v", ok, err)
	}

	reloaded, err := Open(path, 10)
	if err != nil {
		t.Fatal(err)
	}
	got := reloaded.List()[0]
	if got.ControlStatus != "accepted" || got.TransferStatus != "interrupted" || got.Transferred != 40 {
		t.Fatalf("reloaded active record=%+v", got)
	}
}

func TestHistoryControlErrorDoesNotOverwriteActiveTransfer(t *testing.T) {
	store := NewMemory(10)
	pkg := pkgstore.Package{ID: "pkg-3", Name: "Game.pkg", RelativePath: "Game.pkg", Size: 100}
	record, err := store.Create(pkg, "http://nas/pkg/pkg-3")
	if err != nil {
		t.Fatal(err)
	}
	if _, ok, err := store.UpdateTransfer(pkg.ID, "downloading", 30, 100, 1); err != nil || !ok {
		t.Fatalf("UpdateTransfer ok=%v err=%v", ok, err)
	}
	got, ok, err := store.MarkControlError(record.ID, errors.New("receiver timeout"))
	if err != nil || !ok {
		t.Fatalf("MarkControlError ok=%v err=%v", ok, err)
	}
	if got.ControlStatus != "error" || got.TransferStatus != "downloading" || got.ControlError != "receiver timeout" {
		t.Fatalf("record=%+v", got)
	}
}

func TestHistoryTrimsOldRecords(t *testing.T) {
	store := NewMemory(2)
	now := time.Date(2026, 9, 23, 1, 0, 0, 0, time.UTC)
	store.now = func() time.Time {
		now = now.Add(time.Second)
		return now
	}
	for _, id := range []string{"a", "b", "c"} {
		if _, err := store.Create(pkgstore.Package{ID: id, Name: id + ".pkg", RelativePath: id + ".pkg", Size: 1}, "http://nas/pkg/"+id); err != nil {
			t.Fatal(err)
		}
	}
	records := store.List()
	if len(records) != 2 || records[0].PackageID != "c" || records[1].PackageID != "b" {
		t.Fatalf("records=%+v", records)
	}
}

func TestQueueFIFOAndLifecycle(t *testing.T) {
	store := NewMemory(10)
	now := time.Date(2026, 9, 23, 5, 0, 0, 0, time.UTC)
	store.now = func() time.Time {
		current := now
		now = now.Add(time.Second)
		return current
	}
	firstPkg := pkgstore.Package{ID: "first", Name: "First.pkg", RelativePath: "First.pkg", Size: 10}
	secondPkg := pkgstore.Package{ID: "second", Name: "Second.pkg", RelativePath: "Second.pkg", Size: 20}
	first, err := store.CreateQueued(firstPkg, "http://nas/pkg/first", "")
	if err != nil {
		t.Fatal(err)
	}
	second, err := store.CreateQueued(secondPkg, "http://nas/pkg/second", first.ID)
	if err != nil {
		t.Fatal(err)
	}
	if second.RetryOf != first.ID {
		t.Fatalf("retryOf=%q, want %q", second.RetryOf, first.ID)
	}
	if !store.HasPendingPackage(firstPkg.ID) || !store.HasPendingPackage(secondPkg.ID) {
		t.Fatal("queued packages must be reported as pending")
	}

	claimed, ok, err := store.ClaimNextQueued()
	if err != nil || !ok {
		t.Fatalf("ClaimNextQueued ok=%v err=%v", ok, err)
	}
	if claimed.ID != first.ID || claimed.QueueStatus != QueueSubmitting || claimed.ControlStatus != "requesting" || claimed.TransferStatus != "waiting" {
		t.Fatalf("claimed first=%+v", claimed)
	}
	if _, ok, err := store.ClaimNextQueued(); err != nil || ok {
		t.Fatalf("active submission must block the next claim: ok=%v err=%v", ok, err)
	}

	accepted, ok, err := store.MarkAccepted(first.ID)
	if err != nil || !ok {
		t.Fatalf("MarkAccepted ok=%v err=%v", ok, err)
	}
	if accepted.QueueStatus != QueueActive || accepted.ControlStatus != "accepted" {
		t.Fatalf("accepted=%+v", accepted)
	}
	if _, ok, err := store.ClaimNextQueued(); err != nil || ok {
		t.Fatalf("active transfer must block the next claim: ok=%v err=%v", ok, err)
	}

	completed, ok, err := store.UpdateTransfer(firstPkg.ID, "complete", 10, 10, 1)
	if err != nil || !ok {
		t.Fatalf("UpdateTransfer ok=%v err=%v", ok, err)
	}
	if completed.QueueStatus != QueueComplete || completed.TransferStatus != "complete" {
		t.Fatalf("completed=%+v", completed)
	}
	if store.HasPendingPackage(firstPkg.ID) {
		t.Fatal("completed package must no longer be pending")
	}

	claimed, ok, err = store.ClaimNextQueued()
	if err != nil || !ok {
		t.Fatalf("second ClaimNextQueued ok=%v err=%v", ok, err)
	}
	if claimed.ID != second.ID {
		t.Fatalf("claimed second id=%q, want %q", claimed.ID, second.ID)
	}
}

func TestQueueRestartKeepsQueuedButInterruptsSubmitting(t *testing.T) {
	path := filepath.Join(t.TempDir(), "history.json")
	store, err := Open(path, 10)
	if err != nil {
		t.Fatal(err)
	}
	firstPkg := pkgstore.Package{ID: "first", Name: "First.pkg", RelativePath: "First.pkg", Size: 10}
	secondPkg := pkgstore.Package{ID: "second", Name: "Second.pkg", RelativePath: "Second.pkg", Size: 20}
	first, err := store.CreateQueued(firstPkg, "http://nas/pkg/first", "")
	if err != nil {
		t.Fatal(err)
	}
	second, err := store.CreateQueued(secondPkg, "http://nas/pkg/second", "")
	if err != nil {
		t.Fatal(err)
	}
	claimed, ok, err := store.ClaimNextQueued()
	if err != nil || !ok || claimed.ID != first.ID {
		t.Fatalf("ClaimNextQueued=%+v ok=%v err=%v", claimed, ok, err)
	}

	reloaded, err := Open(path, 10)
	if err != nil {
		t.Fatal(err)
	}
	gotFirst, ok := reloaded.Get(first.ID)
	if !ok {
		t.Fatal("missing interrupted record")
	}
	if gotFirst.QueueStatus != QueueInterrupted || gotFirst.ControlStatus != "interrupted" || gotFirst.TransferStatus != "interrupted" {
		t.Fatalf("interrupted=%+v", gotFirst)
	}
	gotSecond, ok := reloaded.Get(second.ID)
	if !ok {
		t.Fatal("missing queued record")
	}
	if gotSecond.QueueStatus != QueueQueued || gotSecond.ControlStatus != "pending" || gotSecond.TransferStatus != "not_started" {
		t.Fatalf("queued after restart=%+v", gotSecond)
	}
	claimed, ok, err = reloaded.ClaimNextQueued()
	if err != nil || !ok || claimed.ID != second.ID {
		t.Fatalf("queued task did not resume safely: claimed=%+v ok=%v err=%v", claimed, ok, err)
	}
}

func TestQueueRestartInterruptsAcceptedActiveWithoutChangingAcceptance(t *testing.T) {
	path := filepath.Join(t.TempDir(), "history.json")
	store, err := Open(path, 10)
	if err != nil {
		t.Fatal(err)
	}
	pkg := pkgstore.Package{ID: "pkg", Name: "Game.pkg", RelativePath: "Game.pkg", Size: 100}
	record, err := store.CreateQueued(pkg, "http://nas/pkg/pkg", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, ok, err := store.ClaimNextQueued(); err != nil || !ok {
		t.Fatalf("ClaimNextQueued ok=%v err=%v", ok, err)
	}
	if _, ok, err := store.MarkAccepted(record.ID); err != nil || !ok {
		t.Fatalf("MarkAccepted ok=%v err=%v", ok, err)
	}
	if _, ok, err := store.UpdateTransfer(pkg.ID, "downloading", 40, 100, 2); err != nil || !ok {
		t.Fatalf("UpdateTransfer ok=%v err=%v", ok, err)
	}

	reloaded, err := Open(path, 10)
	if err != nil {
		t.Fatal(err)
	}
	got, ok := reloaded.Get(record.ID)
	if !ok {
		t.Fatal("missing active record")
	}
	if got.QueueStatus != QueueInterrupted || got.ControlStatus != "accepted" || got.TransferStatus != "interrupted" || got.Transferred != 40 {
		t.Fatalf("reloaded active record=%+v", got)
	}
}

func TestRetentionNeverDropsPendingQueueRecord(t *testing.T) {
	store := NewMemory(2)
	now := time.Date(2026, 9, 23, 5, 0, 0, 0, time.UTC)
	store.now = func() time.Time {
		current := now
		now = now.Add(time.Second)
		return current
	}
	pending, err := store.CreateQueued(pkgstore.Package{ID: "pending", Name: "Pending.pkg", RelativePath: "Pending.pkg", Size: 1}, "http://nas/pkg/pending", "")
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"a", "b", "c"} {
		if _, err := store.Create(pkgstore.Package{ID: id, Name: id + ".pkg", RelativePath: id + ".pkg", Size: 1}, "http://nas/pkg/"+id); err != nil {
			t.Fatal(err)
		}
	}
	records := store.List()
	if len(records) != 3 {
		t.Fatalf("records=%d, want 3 (limit plus retained pending)", len(records))
	}
	if _, ok := store.Get(pending.ID); !ok {
		t.Fatal("retention dropped a queued task")
	}
}

func TestCreateQueuedPersistenceFailureIsFailClosed(t *testing.T) {
	parentFile := filepath.Join(t.TempDir(), "not-a-directory")
	if err := os.WriteFile(parentFile, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	store := newStore(filepath.Join(parentFile, "history.json"), 10)
	pkg := pkgstore.Package{ID: "pkg", Name: "Game.pkg", RelativePath: "Game.pkg", Size: 1}
	record, err := store.CreateQueued(pkg, "http://nas/pkg/pkg", "")
	if err == nil {
		t.Fatal("expected persistence error")
	}
	if record.ID != "" {
		t.Fatalf("failed enqueue returned record=%+v", record)
	}
	if len(store.List()) != 0 {
		t.Fatalf("failed enqueue remained in memory: %+v", store.List())
	}
}

func TestQueueCompletePersistenceFailureDoesNotReleaseNextSlot(t *testing.T) {
	path := filepath.Join(t.TempDir(), "history.json")
	store, err := Open(path, 10)
	if err != nil {
		t.Fatal(err)
	}
	firstPkg := pkgstore.Package{ID: "first", Name: "First.pkg", RelativePath: "First.pkg", Size: 10}
	secondPkg := pkgstore.Package{ID: "second", Name: "Second.pkg", RelativePath: "Second.pkg", Size: 10}
	first, err := store.CreateQueued(firstPkg, "http://nas/pkg/first", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.CreateQueued(secondPkg, "http://nas/pkg/second", ""); err != nil {
		t.Fatal(err)
	}
	claimed, ok, err := store.ClaimNextQueued()
	if err != nil || !ok || claimed.ID != first.ID {
		t.Fatalf("ClaimNextQueued=%+v ok=%v err=%v", claimed, ok, err)
	}
	if _, ok, err := store.MarkAccepted(first.ID); err != nil || !ok {
		t.Fatalf("MarkAccepted ok=%v err=%v", ok, err)
	}

	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(path, 0o700); err != nil {
		t.Fatal(err)
	}
	got, ok, err := store.UpdateTransfer(firstPkg.ID, "complete", 10, 10, 1)
	if err == nil || !ok {
		t.Fatalf("UpdateTransfer ok=%v err=%v, want persistence error", ok, err)
	}
	if got.QueueStatus != QueueActive || got.TransferStatus != "waiting" {
		t.Fatalf("failed complete advanced in-memory state: %+v", got)
	}
	current, _ := store.Get(first.ID)
	if current.QueueStatus != QueueActive || current.TransferStatus != "waiting" {
		t.Fatalf("stored in-memory record advanced after persistence failure: %+v", current)
	}
	if _, ok, err := store.ClaimNextQueued(); err != nil || ok {
		t.Fatalf("next queue item was released after failed completion: ok=%v err=%v", ok, err)
	}
}

func TestQueueControlErrorPersistenceFailureDoesNotReleaseNextSlot(t *testing.T) {
	path := filepath.Join(t.TempDir(), "history.json")
	store, err := Open(path, 10)
	if err != nil {
		t.Fatal(err)
	}
	firstPkg := pkgstore.Package{ID: "first", Name: "First.pkg", RelativePath: "First.pkg", Size: 10}
	secondPkg := pkgstore.Package{ID: "second", Name: "Second.pkg", RelativePath: "Second.pkg", Size: 10}
	first, err := store.CreateQueued(firstPkg, "http://nas/pkg/first", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.CreateQueued(secondPkg, "http://nas/pkg/second", ""); err != nil {
		t.Fatal(err)
	}
	claimed, ok, err := store.ClaimNextQueued()
	if err != nil || !ok || claimed.ID != first.ID {
		t.Fatalf("ClaimNextQueued=%+v ok=%v err=%v", claimed, ok, err)
	}

	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(path, 0o700); err != nil {
		t.Fatal(err)
	}
	got, ok, err := store.MarkControlError(first.ID, errors.New("receiver unavailable"))
	if err == nil || !ok {
		t.Fatalf("MarkControlError ok=%v err=%v, want persistence error", ok, err)
	}
	if got.QueueStatus != QueueSubmitting || got.ControlStatus != "requesting" {
		t.Fatalf("failed control error advanced in-memory state: %+v", got)
	}
	current, _ := store.Get(first.ID)
	if current.QueueStatus != QueueSubmitting || current.ControlStatus != "requesting" {
		t.Fatalf("stored in-memory record advanced after persistence failure: %+v", current)
	}
	if _, ok, err := store.ClaimNextQueued(); err != nil || ok {
		t.Fatalf("next queue item was released after failed control-error persistence: ok=%v err=%v", ok, err)
	}
}

func TestCancelQueuedReleasesPendingPackageAndSkipsCancelledRecord(t *testing.T) {
	store := NewMemory(10)
	firstPkg := pkgstore.Package{ID: "first", Name: "First.pkg", RelativePath: "First.pkg", Size: 10}
	secondPkg := pkgstore.Package{ID: "second", Name: "Second.pkg", RelativePath: "Second.pkg", Size: 10}
	first, err := store.CreateQueued(firstPkg, "http://nas/pkg/first", "")
	if err != nil {
		t.Fatal(err)
	}
	second, err := store.CreateQueued(secondPkg, "http://nas/pkg/second", "")
	if err != nil {
		t.Fatal(err)
	}
	if !store.HasPendingPackage(firstPkg.ID) {
		t.Fatal("first queued package should be pending")
	}
	cancelled, changed, err := store.CancelQueued(first.ID)
	if err != nil || !changed {
		t.Fatalf("CancelQueued changed=%v err=%v", changed, err)
	}
	if cancelled.QueueStatus != QueueCancelled {
		t.Fatalf("cancelled=%+v", cancelled)
	}
	if store.HasPendingPackage(firstPkg.ID) {
		t.Fatal("cancelled package must no longer be pending")
	}
	claimed, ok, err := store.ClaimNextQueued()
	if err != nil || !ok {
		t.Fatalf("ClaimNextQueued ok=%v err=%v", ok, err)
	}
	if claimed.ID != second.ID {
		t.Fatalf("claimed id=%q, want second queued id=%q", claimed.ID, second.ID)
	}
}

func TestCancelQueuedRefusesSubmittingOrActiveRecord(t *testing.T) {
	store := NewMemory(10)
	pkg := pkgstore.Package{ID: "pkg", Name: "Game.pkg", RelativePath: "Game.pkg", Size: 10}
	record, err := store.CreateQueued(pkg, "http://nas/pkg/pkg", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, ok, err := store.ClaimNextQueued(); err != nil || !ok {
		t.Fatalf("ClaimNextQueued ok=%v err=%v", ok, err)
	}
	got, changed, err := store.CancelQueued(record.ID)
	if err != nil {
		t.Fatal(err)
	}
	if changed || got.QueueStatus != QueueSubmitting {
		t.Fatalf("submitting cancel changed=%v record=%+v", changed, got)
	}
	if _, ok, err := store.MarkAccepted(record.ID); err != nil || !ok {
		t.Fatalf("MarkAccepted ok=%v err=%v", ok, err)
	}
	got, changed, err = store.CancelQueued(record.ID)
	if err != nil {
		t.Fatal(err)
	}
	if changed || got.QueueStatus != QueueActive {
		t.Fatalf("active cancel changed=%v record=%+v", changed, got)
	}
}

func TestCancelQueuedPersistenceFailureRollsBack(t *testing.T) {
	path := filepath.Join(t.TempDir(), "history.json")
	store, err := Open(path, 10)
	if err != nil {
		t.Fatal(err)
	}
	pkg := pkgstore.Package{ID: "pkg", Name: "Game.pkg", RelativePath: "Game.pkg", Size: 10}
	record, err := store.CreateQueued(pkg, "http://nas/pkg/pkg", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(path, 0o700); err != nil {
		t.Fatal(err)
	}
	got, changed, err := store.CancelQueued(record.ID)
	if err == nil || changed {
		t.Fatalf("CancelQueued changed=%v err=%v, want persistence failure", changed, err)
	}
	if got.QueueStatus != QueueQueued {
		t.Fatalf("failed cancel advanced returned state: %+v", got)
	}
	current, ok := store.Get(record.ID)
	if !ok || current.QueueStatus != QueueQueued || !store.HasPendingPackage(pkg.ID) {
		t.Fatalf("failed cancel did not roll back queued state: %+v ok=%v", current, ok)
	}
}

func TestMoveQueuedChangesFIFOClaimOrder(t *testing.T) {
	store := NewMemory(10)
	pkgs := []pkgstore.Package{
		{ID: "a", Name: "A.pkg", RelativePath: "A.pkg", Size: 1},
		{ID: "b", Name: "B.pkg", RelativePath: "B.pkg", Size: 1},
		{ID: "c", Name: "C.pkg", RelativePath: "C.pkg", Size: 1},
	}
	records := make([]Record, 0, len(pkgs))
	for _, pkg := range pkgs {
		record, err := store.CreateQueued(pkg, "http://nas/pkg/"+pkg.ID, "")
		if err != nil {
			t.Fatal(err)
		}
		records = append(records, record)
	}
	if records[0].QueueOrder != 1 || records[1].QueueOrder != 2 || records[2].QueueOrder != 3 {
		t.Fatalf("initial queue orders=%+v", records)
	}

	moved, changed, err := store.MoveQueued(records[2].ID, "up")
	if err != nil || !changed {
		t.Fatalf("first MoveQueued changed=%v err=%v", changed, err)
	}
	if moved.QueueOrder != 2 {
		t.Fatalf("C order after first move=%d, want 2", moved.QueueOrder)
	}
	moved, changed, err = store.MoveQueued(records[2].ID, "up")
	if err != nil || !changed {
		t.Fatalf("second MoveQueued changed=%v err=%v", changed, err)
	}
	if moved.QueueOrder != 1 {
		t.Fatalf("C order after second move=%d, want 1", moved.QueueOrder)
	}
	if _, changed, err := store.MoveQueued(records[2].ID, "up"); err != nil || changed {
		t.Fatalf("moving first queued item above boundary changed=%v err=%v", changed, err)
	}

	claimed, ok, err := store.ClaimNextQueued()
	if err != nil || !ok {
		t.Fatalf("ClaimNextQueued ok=%v err=%v", ok, err)
	}
	if claimed.ID != records[2].ID {
		t.Fatalf("claimed id=%q, want reordered C id=%q", claimed.ID, records[2].ID)
	}
}

func TestMoveQueuedRefusesNonQueuedRecord(t *testing.T) {
	store := NewMemory(10)
	first, err := store.CreateQueued(pkgstore.Package{ID: "a", Name: "A.pkg", RelativePath: "A.pkg", Size: 1}, "http://nas/pkg/a", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.CreateQueued(pkgstore.Package{ID: "b", Name: "B.pkg", RelativePath: "B.pkg", Size: 1}, "http://nas/pkg/b", ""); err != nil {
		t.Fatal(err)
	}
	if _, ok, err := store.ClaimNextQueued(); err != nil || !ok {
		t.Fatalf("ClaimNextQueued ok=%v err=%v", ok, err)
	}
	got, changed, err := store.MoveQueued(first.ID, "down")
	if err != nil {
		t.Fatal(err)
	}
	if changed || got.QueueStatus != QueueSubmitting {
		t.Fatalf("submitting move changed=%v record=%+v", changed, got)
	}
}

func TestMoveQueuedPersistenceFailureRollsBackOrders(t *testing.T) {
	path := filepath.Join(t.TempDir(), "history.json")
	store, err := Open(path, 10)
	if err != nil {
		t.Fatal(err)
	}
	first, err := store.CreateQueued(pkgstore.Package{ID: "a", Name: "A.pkg", RelativePath: "A.pkg", Size: 1}, "http://nas/pkg/a", "")
	if err != nil {
		t.Fatal(err)
	}
	second, err := store.CreateQueued(pkgstore.Package{ID: "b", Name: "B.pkg", RelativePath: "B.pkg", Size: 1}, "http://nas/pkg/b", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(path, 0o700); err != nil {
		t.Fatal(err)
	}
	got, changed, err := store.MoveQueued(second.ID, "up")
	if err == nil || changed {
		t.Fatalf("MoveQueued changed=%v err=%v, want persistence failure", changed, err)
	}
	if got.QueueOrder != second.QueueOrder {
		t.Fatalf("failed move returned order=%d, want %d", got.QueueOrder, second.QueueOrder)
	}
	currentFirst, _ := store.Get(first.ID)
	currentSecond, _ := store.Get(second.ID)
	if currentFirst.QueueOrder != first.QueueOrder || currentSecond.QueueOrder != second.QueueOrder {
		t.Fatalf("queue orders changed after persistence failure: first=%+v second=%+v", currentFirst, currentSecond)
	}
}

func TestOpenMigratesLegacyQueuedRecordsWithoutQueueOrder(t *testing.T) {
	path := filepath.Join(t.TempDir(), "history.json")
	store, err := Open(path, 10)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 9, 23, 7, 0, 0, 0, time.UTC)
	store.now = func() time.Time {
		current := now
		now = now.Add(time.Second)
		return current
	}
	first, err := store.CreateQueued(pkgstore.Package{ID: "a", Name: "A.pkg", RelativePath: "A.pkg", Size: 1}, "http://nas/pkg/a", "")
	if err != nil {
		t.Fatal(err)
	}
	second, err := store.CreateQueued(pkgstore.Package{ID: "b", Name: "B.pkg", RelativePath: "B.pkg", Size: 1}, "http://nas/pkg/b", "")
	if err != nil {
		t.Fatal(err)
	}

	store.mu.Lock()
	for i := range store.records {
		if store.records[i].QueueStatus == QueueQueued {
			store.records[i].QueueOrder = 0
		}
	}
	if err := store.persistLocked(); err != nil {
		store.mu.Unlock()
		t.Fatal(err)
	}
	store.mu.Unlock()

	reloaded, err := Open(path, 10)
	if err != nil {
		t.Fatal(err)
	}
	gotFirst, ok := reloaded.Get(first.ID)
	if !ok {
		t.Fatal("missing first migrated record")
	}
	gotSecond, ok := reloaded.Get(second.ID)
	if !ok {
		t.Fatal("missing second migrated record")
	}
	if gotFirst.QueueOrder != 1 || gotSecond.QueueOrder != 2 {
		t.Fatalf("migrated orders first=%d second=%d, want 1,2", gotFirst.QueueOrder, gotSecond.QueueOrder)
	}
	claimed, ok, err := reloaded.ClaimNextQueued()
	if err != nil || !ok || claimed.ID != first.ID {
		t.Fatalf("legacy FIFO changed after migration: claimed=%+v ok=%v err=%v", claimed, ok, err)
	}
}

func TestUnavailableStoreRejectsQueueWrites(t *testing.T) {
	store := NewUnavailable(errors.New("corrupt history"))
	pkg := pkgstore.Package{ID: "pkg", Name: "Game.pkg", RelativePath: "Game.pkg", Size: 1}
	if _, err := store.CreateQueued(pkg, "http://nas/pkg/pkg", ""); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("CreateQueued err=%v, want ErrUnavailable", err)
	}
	if _, ok, err := store.ClaimNextQueued(); ok || !errors.Is(err, ErrUnavailable) {
		t.Fatalf("ClaimNextQueued ok=%v err=%v, want unavailable", ok, err)
	}
	if records := store.List(); len(records) != 0 {
		t.Fatalf("unavailable store records=%+v", records)
	}
}

func TestHistoryRejectsCorruptFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "history.json")
	if err := os.WriteFile(path, []byte("{not-json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(path, 10); err == nil {
		t.Fatal("expected corrupt history file error")
	}
}
