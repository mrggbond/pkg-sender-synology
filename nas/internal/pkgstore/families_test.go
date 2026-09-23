package pkgstore

import (
	"testing"

	"github.com/Loopayeh/pkg-sender/nas/internal/pkgmeta"
)

func TestBuildFamiliesGroupsByTitleIDAndPrefersGameTitle(t *testing.T) {
	packages := []Package{
		{
			ID: "dlc", Name: "DLC.pkg", RelativePath: "Game/DLC.pkg", Size: 25,
			Metadata: pkgmeta.Metadata{
				Title: "Game Extra Content", TitleID: "PPSA10000", Platform: "PS5",
				PackageType: "dlc", PackageTypeSource: "heuristic",
			},
		},
		{
			ID: "game", Name: "Game.pkg", RelativePath: "Game/Game.pkg", Size: 100,
			Metadata: pkgmeta.Metadata{
				Title: "Example Game", TitleID: "PPSA10000", Platform: "PS5",
				PackageType: "game", PackageTypeSource: "param", Version: "01.000.000",
			},
		},
		{
			ID: "patch", Name: "Patch.pkg", RelativePath: "Game/Patch.pkg", Size: 50,
			Metadata: pkgmeta.Metadata{
				Title: "Example Game Update", TitleID: "PPSA10000", Platform: "PS5",
				PackageType: "patch", PackageTypeSource: "structure", Version: "01.002.000",
			},
		},
	}

	families := BuildFamilies(packages)
	if len(families) != 1 {
		t.Fatalf("families=%d, want 1", len(families))
	}
	family := families[0]
	if family.ID != "PPSA10000" || family.TitleID != "PPSA10000" || family.Title != "Example Game" {
		t.Fatalf("unexpected family identity: %+v", family)
	}
	if family.PackageCount != 3 || family.TotalSize != 175 {
		t.Fatalf("unexpected family totals: %+v", family)
	}
	if got := []string{family.Packages[0].ID, family.Packages[1].ID, family.Packages[2].ID}; got[0] != "game" || got[1] != "patch" || got[2] != "dlc" {
		t.Fatalf("package order=%v, want game, patch, dlc", got)
	}
}

func TestBuildFamiliesKeepsUnknownTitleIDPackagesSeparate(t *testing.T) {
	packages := []Package{
		{ID: "one", Name: "One.pkg", RelativePath: "One.pkg", Size: 1},
		{ID: "two", Name: "Two.pkg", RelativePath: "Two.pkg", Size: 2},
	}
	families := BuildFamilies(packages)
	if len(families) != 2 {
		t.Fatalf("families=%d, want 2", len(families))
	}
	ids := map[string]bool{}
	for _, family := range families {
		ids[family.ID] = true
		if family.PackageCount != 1 {
			t.Fatalf("fallback family grouped unrelated packages: %+v", family)
		}
	}
	if !ids["one"] || !ids["two"] {
		t.Fatalf("fallback ids=%v", ids)
	}
}

func TestBuildFamiliesSortsPatchVersionsNewestFirst(t *testing.T) {
	packages := []Package{
		{ID: "old", Metadata: pkgmeta.Metadata{TitleID: "PPSA20000", Title: "Game Update", PackageType: "patch", Version: "01.002.000"}},
		{ID: "new", Metadata: pkgmeta.Metadata{TitleID: "PPSA20000", Title: "Game Update", PackageType: "patch", Version: "01.010.000"}},
	}
	families := BuildFamilies(packages)
	if len(families) != 1 || len(families[0].Packages) != 2 {
		t.Fatalf("unexpected families: %+v", families)
	}
	if families[0].Packages[0].ID != "new" || families[0].Packages[1].ID != "old" {
		t.Fatalf("patch order=%v, %v", families[0].Packages[0].Version, families[0].Packages[1].Version)
	}
}
