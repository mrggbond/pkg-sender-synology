package pkgstore

import (
	"os"
	"path/filepath"
	"testing"
)

func TestScanFindsNestedPKGFilesOnly(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "nested"), 0o755); err != nil {
		t.Fatal(err)
	}
	mustWrite(t, filepath.Join(root, "A.pkg"), "a")
	mustWrite(t, filepath.Join(root, "nested", "B.PKG"), "bb")
	mustWrite(t, filepath.Join(root, "ignore.txt"), "no")

	store, err := New(root)
	if err != nil {
		t.Fatal(err)
	}
	count, err := store.Scan()
	if err != nil {
		t.Fatal(err)
	}
	if count != 2 {
		t.Fatalf("count=%d, want 2", count)
	}

	got := store.List()
	if len(got) != 2 {
		t.Fatalf("len=%d, want 2", len(got))
	}
	if got[0].RelativePath != "A.pkg" || got[1].RelativePath != "nested/B.PKG" {
		t.Fatalf("unexpected paths: %#v", got)
	}
	if got[0].ID == "" || got[0].ID == got[1].ID {
		t.Fatalf("invalid ids: %#v", got)
	}

	firstID := got[0].ID
	if _, err := store.Scan(); err != nil {
		t.Fatal(err)
	}
	if store.List()[0].ID != firstID {
		t.Fatalf("stable path ID changed across rescans")
	}
}

func mustWrite(t *testing.T, path, body string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}
