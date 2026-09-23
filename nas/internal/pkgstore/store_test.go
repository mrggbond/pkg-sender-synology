package pkgstore

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Loopayeh/pkg-sender/nas/internal/pkgmeta"
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
	if got[0].MetadataParsed || got[1].MetadataParsed {
		t.Fatalf("plain test files should remain scannable with metadataParsed=false: %#v", got)
	}

	firstID := got[0].ID
	if _, err := store.Scan(); err != nil {
		t.Fatal(err)
	}
	if store.List()[0].ID != firstID {
		t.Fatalf("stable path ID changed across rescans")
	}
}

func TestPackageJSONFlattensMetadataFields(t *testing.T) {
	category := uint32(0)
	pkg := Package{
		ID:             "id",
		Name:           "Game.pkg",
		RelativePath:   "Game.pkg",
		Size:           123,
		MetadataParsed: true,
		Metadata: pkgmeta.Metadata{
			Title:                   "Game",
			TitleID:                 "PPSA12345",
			ContentID:               "UP0001-PPSA12345_00-EXAMPLEGAME00001",
			Version:                 "01.000.000",
			PackageType:             "game",
			PackageTypeSource:       "param",
			ApplicationCategoryType: &category,
		},
	}
	data, err := json.Marshal(pkg)
	if err != nil {
		t.Fatal(err)
	}
	got := string(data)
	for _, want := range []string{
		`"metadataParsed":true`,
		`"title":"Game"`,
		`"titleId":"PPSA12345"`,
		`"contentId":"UP0001-PPSA12345_00-EXAMPLEGAME00001"`,
		`"packageType":"game"`,
		`"applicationCategoryType":0`,
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("json %s does not contain %s", got, want)
		}
	}
}

func mustWrite(t *testing.T, path, body string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}
