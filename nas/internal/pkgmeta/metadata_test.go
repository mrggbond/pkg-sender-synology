package pkgmeta

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"testing"
)

type fixtureEntry struct {
	id    uint32
	flags uint32
	data  []byte
}

func TestReadPS5FIHMetadata(t *testing.T) {
	param, err := json.Marshal(map[string]any{
		"contentId":               "UP0001-PPSA12345_00-EXAMPLEGAME00001",
		"titleId":                 "PPSA12345",
		"contentVersion":          "01.234.000",
		"masterVersion":           "01.00",
		"applicationCategoryType": 0,
		"localizedParameters": map[string]any{
			"defaultLanguage": "zh-Hans",
			"zh-Hans":         map[string]any{"titleName": "测试游戏"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	data := buildFixture(t, true, "UP0001-PPSA12345_00-EXAMPLEGAME00001", []fixtureEntry{
		{id: 0x2000, data: param},
	})
	meta, err := Read(bytes.NewReader(data), int64(len(data)), "Game.pkg")
	if err != nil {
		t.Fatal(err)
	}
	if meta.Title != "测试游戏" ||
		meta.TitleID != "PPSA12345" ||
		meta.ContentID != "UP0001-PPSA12345_00-EXAMPLEGAME00001" ||
		meta.Version != "01.234.000" ||
		meta.MasterVersion != "01.00" ||
		meta.Platform != "PS5" ||
		meta.PackageType != "game" ||
		meta.PackageTypeSource != "param" {
		t.Fatalf("unexpected metadata: %+v", meta)
	}
	if meta.ApplicationCategoryType == nil || *meta.ApplicationCategoryType != 0 {
		t.Fatalf("application category=%v", meta.ApplicationCategoryType)
	}
}

func TestReadPatchByStructure(t *testing.T) {
	param := []byte(`{
		"titleId":"PPSA54321",
		"contentVersion":"02.000.001",
		"targetContentVersion":"02.000.000",
		"applicationCategoryType":0,
		"localizedParameters":{"defaultLanguage":"en-US","en-US":{"titleName":"Patch Game"}}
	}`)
	data := buildFixture(t, false, "UP0001-PPSA54321_00-PATCHGAME0000001", []fixtureEntry{
		{id: 0x2000, data: param},
		{id: 0x0407, data: []byte{1}},
	})
	meta, err := Read(bytes.NewReader(data), int64(len(data)), "Patch.pkg")
	if err != nil {
		t.Fatal(err)
	}
	if meta.PackageType != "patch" || meta.PackageTypeSource != "structure" {
		t.Fatalf("unexpected patch classification: %+v", meta)
	}
	if meta.TargetVersion != "02.000.000" {
		t.Fatalf("target version=%q", meta.TargetVersion)
	}
}

func TestReadDLCHeuristicWithoutParamJSON(t *testing.T) {
	data := buildFixture(t, false, "UP0001-PPSA22222_00-SOMECONTENT00001", []fixtureEntry{
		{id: 0x1200, data: []byte("not-an-icon")},
	})
	meta, err := Read(bytes.NewReader(data), int64(len(data)), "Game-DLC.pkg")
	if err != nil {
		t.Fatal(err)
	}
	if meta.TitleID != "PPSA22222" {
		t.Fatalf("title id=%q", meta.TitleID)
	}
	if meta.PackageType != "dlc" || meta.PackageTypeSource != "heuristic" {
		t.Fatalf("unexpected dlc classification: %+v", meta)
	}
}

func TestReadIconPrefersExactIconEntry(t *testing.T) {
	exact := append([]byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'}, []byte("exact")...)
	variant := append([]byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'}, []byte("variant")...)
	data := buildFixture(t, true, "UP0001-PPSA11111_00-ICONTEST00000001", []fixtureEntry{
		{id: 0x1201, data: variant},
		{id: 0x1200, data: exact},
	})
	icon, err := ReadIcon(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(icon, exact) {
		t.Fatalf("icon=%q, want exact icon", icon)
	}
}

func TestReadIconSkipsEncryptedExactAndUsesVariant(t *testing.T) {
	encrypted := append([]byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'}, []byte("encrypted")...)
	variant := append([]byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'}, []byte("variant")...)
	data := buildFixture(t, false, "UP0001-PPSA11112_00-ICONTEST00000002", []fixtureEntry{
		{id: 0x1200, flags: 0x80000000, data: encrypted},
		{id: 0x1201, data: variant},
	})
	icon, err := ReadIcon(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(icon, variant) {
		t.Fatalf("icon=%q, want variant icon", icon)
	}
}

func TestReadIconRejectsInvalidPNG(t *testing.T) {
	data := buildFixture(t, false, "UP0001-PPSA11113_00-ICONTEST00000003", []fixtureEntry{
		{id: 0x1200, data: []byte("not-a-png")},
	})
	if _, err := ReadIcon(bytes.NewReader(data), int64(len(data))); err == nil {
		t.Fatal("expected invalid icon to be rejected")
	}
}

func TestReadInvalidFileFails(t *testing.T) {
	data := make([]byte, 0x600)
	if _, err := Read(bytes.NewReader(data), int64(len(data)), "bad.pkg"); err == nil {
		t.Fatal("expected invalid package error")
	}
}

func buildFixture(t *testing.T, fih bool, contentID string, entries []fixtureEntry) []byte {
	t.Helper()
	const cntBaseFIH = 0x1000
	cntBase := 0
	if fih {
		cntBase = cntBaseFIH
	}

	tableOff := 0x600
	nextData := 0x800
	total := cntBase + nextData
	for _, e := range entries {
		total += len(e.data) + 0x20
	}
	total += 0x100
	out := make([]byte, total)

	if fih {
		copy(out[:4], []byte{0x7f, 'F', 'I', 'H'})
		binary.LittleEndian.PutUint64(out[0x58:0x60], uint64(cntBase))
	}

	header := out[cntBase : cntBase+cntHeaderSize]
	copy(header[:4], []byte{0x7f, 'C', 'N', 'T'})
	binary.BigEndian.PutUint32(header[0x10:0x14], uint32(len(entries)))
	binary.BigEndian.PutUint32(header[0x18:0x1c], uint32(tableOff))
	copy(header[0x40:0x70], []byte(contentID))

	for i, e := range entries {
		o := cntBase + tableOff + i*entrySize
		binary.BigEndian.PutUint32(out[o:o+4], e.id)
		binary.BigEndian.PutUint32(out[o+8:o+12], e.flags)
		binary.BigEndian.PutUint32(out[o+0x10:o+0x14], uint32(nextData))
		binary.BigEndian.PutUint32(out[o+0x14:o+0x18], uint32(len(e.data)))
		copy(out[cntBase+nextData:], e.data)
		nextData += len(e.data) + 0x20
	}
	return out
}
