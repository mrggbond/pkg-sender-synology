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

func TestScanSupportsMultipleLibraryRoots(t *testing.T) {
	first := t.TempDir()
	second := t.TempDir()
	mustWrite(t, filepath.Join(first, "Game.pkg"), "a")
	mustWrite(t, filepath.Join(second, "Game.pkg"), "b")
	mustWrite(t, filepath.Join(second, "DLC.pkg"), "c")

	store, err := NewWithRoots([]string{first, second})
	if err != nil {
		t.Fatal(err)
	}
	count, err := store.Scan()
	if err != nil {
		t.Fatal(err)
	}
	if count != 3 {
		t.Fatalf("count=%d, want 3", count)
	}
	got := store.List()
	if len(got) != 3 {
		t.Fatalf("len=%d, want 3", len(got))
	}
	seenIDs := map[string]bool{}
	seenRoots := map[string]bool{}
	for _, pkg := range got {
		if pkg.Path == "" || !strings.Contains(pkg.Path, pkg.RelativePath) {
			t.Fatalf("package path does not include relative path: %+v", pkg)
		}
		if seenIDs[pkg.ID] {
			t.Fatalf("duplicate package id for multi-root scan: %+v", got)
		}
		seenIDs[pkg.ID] = true
		seenRoots[pkg.LibraryRoot] = true
	}
	if !seenRoots[filepath.ToSlash(first)] || !seenRoots[filepath.ToSlash(second)] {
		t.Fatalf("missing expected library roots: %+v", got)
	}
	if roots := store.Roots(); len(roots) != 2 || roots[0] != filepath.Clean(first) || roots[1] != filepath.Clean(second) {
		t.Fatalf("roots=%+v", roots)
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
			Title:          "遊戲",
			DisplayTitle:   "Game",
			SecondaryTitle: "遊戲",
			LocalizedTitles: map[string]string{
				"en-US":   "Game",
				"zh-Hant": "遊戲",
			},
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
		`"title":"遊戲"`,
		`"displayTitle":"Game"`,
		`"secondaryTitle":"遊戲"`,
		`"localizedTitles":{"en-US":"Game"`,
		`"zh-Hant":"遊戲"`,
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

func TestTitleAliasesFallbackAndMissingExport(t *testing.T) {
	aliases := TitleAliases{
		"ppsa10000":                            {"zh-Hans": "中文别名"},
		"UP0001-PPSA30000_00-CONTENT000000001": {"zh-Hant": "內容別名"},
	}.normalized()

	withJapanese := pkgmeta.Metadata{
		TitleID:        "PPSA10000",
		DisplayTitle:   "English Title",
		SecondaryTitle: "日本語タイトル",
	}
	aliases.apply(&withJapanese)
	if withJapanese.SecondaryTitle != "日本語タイトル" {
		t.Fatalf("alias must not override PKG Japanese subtitle: %+v", withJapanese)
	}

	withoutSubtitle := pkgmeta.Metadata{
		ContentID:    "UP0001-PPSA30000_00-CONTENT000000001",
		DisplayTitle: "Content Title",
	}
	aliases.apply(&withoutSubtitle)
	if withoutSubtitle.SecondaryTitle != "內容別名" {
		t.Fatalf("alias fallback failed: %+v", withoutSubtitle)
	}

	store := &Store{list: []Package{
		{ID: "missing", MetadataParsed: true, Metadata: pkgmeta.Metadata{TitleID: "PPSA20000", DisplayTitle: "Missing", LocalizedTitles: map[string]string{"en-US": "Missing"}}},
		{ID: "aliased", MetadataParsed: true, Metadata: withoutSubtitle},
		{ID: "japanese", MetadataParsed: true, Metadata: withJapanese},
	}}
	missing := store.MissingTitleAliases()
	if len(missing) != 1 {
		t.Fatalf("missing aliases=%+v, want one missing title", missing)
	}
	if missing[0].TitleID != "PPSA20000" || missing[0].DisplayTitle != "Missing" || missing[0].PackageCount != 1 {
		t.Fatalf("unexpected missing alias record: %+v", missing[0])
	}
	if got := strings.Join(missing[0].LocalizedLanguages, ","); got != "en-US" {
		t.Fatalf("localized languages=%q", got)
	}

	export := BuildTitleAliasExport(missing)
	if len(export.Missing) != 1 || export.Missing[0].TitleID != "PPSA20000" {
		t.Fatalf("export missing=%+v", export.Missing)
	}
	if export.AIPrompt == "" || !strings.Contains(export.AIPrompt, "PPSA20000") || !strings.Contains(export.AIPrompt, "只返回 JSON") {
		t.Fatalf("unexpected AI prompt: %q", export.AIPrompt)
	}
	if _, ok := export.AliasTemplate["_aiPrompt"].(string); !ok {
		t.Fatalf("alias template missing _aiPrompt: %+v", export.AliasTemplate)
	}
	if _, ok := export.AliasTemplate["PPSA20000"]; !ok {
		t.Fatalf("alias template missing title id: %+v", export.AliasTemplate)
	}

	aliasPath := filepath.Join(t.TempDir(), "aliases.json")
	if err := os.WriteFile(aliasPath, []byte(`{
		"_aiPrompt":"ignored prompt text",
		"PPSA20000":{"zh-Hans":"简体别名"}
	}`), 0o600); err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadTitleAliasesFile(aliasPath)
	if err != nil {
		t.Fatal(err)
	}
	loadedMeta := pkgmeta.Metadata{TitleID: "PPSA20000", DisplayTitle: "Missing"}
	loaded.apply(&loadedMeta)
	if loadedMeta.SecondaryTitle != "简体别名" {
		t.Fatalf("alias file with _aiPrompt did not load: %+v", loadedMeta)
	}

	imported, result, err := ImportTitleAliasesFile(aliasPath, []byte(`{
		"aliasTemplate":{
			"_aiPrompt":"new prompt",
			"PPSA30000":{"zh-Hans":"新增别名"}
		}
	}`))
	if err != nil {
		t.Fatal(err)
	}
	if result.Titles != 2 || result.Languages != 2 {
		t.Fatalf("import result=%+v, want two titles/two languages", result)
	}
	importedMeta := pkgmeta.Metadata{TitleID: "PPSA30000", DisplayTitle: "New Title"}
	imported.apply(&importedMeta)
	if importedMeta.SecondaryTitle != "新增别名" {
		t.Fatalf("imported alias did not apply: %+v", importedMeta)
	}
	reloadedData, err := os.ReadFile(aliasPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(reloadedData), "_aiPrompt") || !strings.Contains(string(reloadedData), "PPSA20000") || !strings.Contains(string(reloadedData), "PPSA30000") {
		t.Fatalf("import did not merge/preserve expected aliases: %s", reloadedData)
	}
}

func mustWrite(t *testing.T, path, body string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}
