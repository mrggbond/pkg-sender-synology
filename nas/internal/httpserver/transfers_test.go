package httpserver

import (
	"errors"
	"math"
	"testing"

	"github.com/Loopayeh/pkg-sender/nas/internal/pkgstore"
)

func TestTransferTrackerMergesDuplicateAndOverlappingRanges(t *testing.T) {
	tracker := newTransferTracker()
	pkg := pkgstore.Package{ID: "pkg-1", Name: "Game.pkg", RelativePath: "Game.pkg", Size: 100}
	tracker.Start(pkg)
	tracker.MarkQueued(pkg.ID)

	first, ok := tracker.Record(pkg.ID, "GET", 206, "bytes 0-49/100", 50)
	if !ok {
		t.Fatal("first range was not tracked")
	}
	if first.Transferred != 50 || first.RangeCount != 1 || first.Status != "downloading" {
		t.Fatalf("first snapshot=%+v", first)
	}

	duplicate, ok := tracker.Record(pkg.ID, "GET", 206, "bytes 0-49/100", 50)
	if !ok {
		t.Fatal("duplicate range was not tracked")
	}
	if duplicate.Transferred != 50 || duplicate.RangeCount != 1 {
		t.Fatalf("duplicate counted twice: %+v", duplicate)
	}

	overlap, ok := tracker.Record(pkg.ID, "GET", 206, "bytes 25-74/100", 50)
	if !ok {
		t.Fatal("overlapping range was not tracked")
	}
	if overlap.Transferred != 75 || overlap.RangeCount != 1 {
		t.Fatalf("overlap union incorrect: %+v", overlap)
	}

	complete, ok := tracker.Record(pkg.ID, "GET", 206, "bytes 75-99/100", 25)
	if !ok {
		t.Fatal("final range was not tracked")
	}
	if complete.Transferred != 100 || complete.RangeCount != 1 || complete.Status != "complete" {
		t.Fatalf("complete snapshot=%+v", complete)
	}
	if math.Abs(complete.Percent-100) > 0.0001 {
		t.Fatalf("percent=%f, want 100", complete.Percent)
	}
}

func TestTransferTrackerUsesActualBytesWritten(t *testing.T) {
	tracker := newTransferTracker()
	pkg := pkgstore.Package{ID: "pkg-2", Name: "Game.pkg", RelativePath: "Game.pkg", Size: 100}
	tracker.Start(pkg)

	snapshot, ok := tracker.Record(pkg.ID, "GET", 206, "bytes 20-79/100", 10)
	if !ok {
		t.Fatal("partial write was not tracked")
	}
	if snapshot.Transferred != 10 {
		t.Fatalf("transferred=%d, want 10", snapshot.Transferred)
	}
}

func TestTransferTrackerResetOnNewInstall(t *testing.T) {
	tracker := newTransferTracker()
	pkg := pkgstore.Package{ID: "pkg-3", Name: "Game.pkg", RelativePath: "Game.pkg", Size: 100}
	tracker.Start(pkg)
	tracker.Record(pkg.ID, "GET", 206, "bytes 0-49/100", 50)

	reset := tracker.Start(pkg)
	if reset.Status != "requesting" || reset.Transferred != 0 || reset.RangeCount != 0 {
		t.Fatalf("reset snapshot=%+v", reset)
	}
}

func TestTransferTrackerDoesNotOverwriteActiveDownloadWithLateControlError(t *testing.T) {
	tracker := newTransferTracker()
	pkg := pkgstore.Package{ID: "pkg-late-error", Name: "Game.pkg", RelativePath: "Game.pkg", Size: 100}
	tracker.Start(pkg)
	before, ok := tracker.Record(pkg.ID, "GET", 206, "bytes 0-49/100", 50)
	if !ok || before.Status != "downloading" {
		t.Fatalf("before=%+v ok=%v", before, ok)
	}

	after, ok := tracker.MarkError(pkg.ID, errors.New("late control-plane error"))
	if !ok {
		t.Fatal("session disappeared")
	}
	if after.Status != "downloading" || after.Transferred != 50 || after.Error != "" {
		t.Fatalf("late error overwrote active transfer: %+v", after)
	}
}

func TestTransferTrackerIgnoresUntrackedAndHeadRequests(t *testing.T) {
	tracker := newTransferTracker()
	if _, ok := tracker.Record("missing", "GET", 206, "bytes 0-9/10", 10); ok {
		t.Fatal("untracked package should not create a session")
	}

	pkg := pkgstore.Package{ID: "pkg-4", Name: "Game.pkg", RelativePath: "Game.pkg", Size: 10}
	tracker.Start(pkg)
	if _, ok := tracker.Record(pkg.ID, "HEAD", 200, "", 10); ok {
		t.Fatal("HEAD request should not count transfer bytes")
	}
}
