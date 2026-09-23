package pkgmeta

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
)

const (
	cntHeaderSize = 0x5a0
	entrySize     = 0x20
	maxEntries    = 0x10000
	maxParamSize  = 2 * 1024 * 1024
	maxIconSize   = 8 * 1024 * 1024
)

type Metadata struct {
	Title                   string  `json:"title,omitempty"`
	TitleID                 string  `json:"titleId,omitempty"`
	ContentID               string  `json:"contentId,omitempty"`
	Version                 string  `json:"version,omitempty"`
	MasterVersion           string  `json:"masterVersion,omitempty"`
	TargetVersion           string  `json:"targetVersion,omitempty"`
	Platform                string  `json:"platform,omitempty"`
	PackageType             string  `json:"packageType,omitempty"`
	PackageTypeSource       string  `json:"packageTypeSource,omitempty"`
	ApplicationCategoryType *uint32 `json:"applicationCategoryType,omitempty"`
}

type entry struct {
	id       uint32
	flags    uint32
	dataOff  uint32
	dataSize uint32
}

type cntImage struct {
	base    int64
	header  []byte
	entries []entry
}

func ReadFile(path string) (Metadata, error) {
	f, err := os.Open(path)
	if err != nil {
		return Metadata{}, err
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil {
		return Metadata{}, err
	}
	if !info.Mode().IsRegular() {
		return Metadata{}, errors.New("not a regular file")
	}
	return Read(f, info.Size(), info.Name())
}

func ReadIconFile(path string) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, errors.New("not a regular file")
	}
	return ReadIcon(f, info.Size())
}

func Read(r io.ReaderAt, size int64, filename string) (Metadata, error) {
	cnt, err := parseContainer(r, size)
	if err != nil {
		return Metadata{}, err
	}
	entries := cnt.entries

	meta := Metadata{
		ContentID: readASCII(cnt.header[0x40:0x70]),
		Platform:  "PS5",
	}
	meta.TitleID = titleIDFromContentID(meta.ContentID)

	var param map[string]json.RawMessage
	if p, ok := findReadableEntry(entries, 0x2000); ok && p.dataSize <= maxParamSize {
		data, readErr := readAt(r, cnt.base+int64(p.dataOff), int64(p.dataSize), size)
		if readErr == nil && json.Unmarshal(data, &param) == nil {
			applyParamJSON(&meta, param)
		}
	}

	patchByStructure := hasReadableEntry(entries, 0x0407) ||
		hasReadableEntry(entries, 0x0408) ||
		hasReadableEntry(entries, 0x1008)

	classify(&meta, filename, patchByStructure)
	if meta.ContentID == "" && meta.TitleID == "" && meta.Title == "" {
		return Metadata{}, errors.New("package metadata not found")
	}
	return meta, nil
}

func ReadIcon(r io.ReaderAt, size int64) ([]byte, error) {
	cnt, err := parseContainer(r, size)
	if err != nil {
		return nil, err
	}

	try := func(e entry) ([]byte, bool) {
		if e.flags&0x80000000 != 0 || e.dataSize == 0 || e.dataSize > maxIconSize {
			return nil, false
		}
		data, readErr := readAt(r, cnt.base+int64(e.dataOff), int64(e.dataSize), size)
		if readErr != nil || !isPNG(data) {
			return nil, false
		}
		return data, true
	}

	for _, e := range cnt.entries {
		if e.id == 0x1200 {
			if data, ok := try(e); ok {
				return data, nil
			}
		}
	}
	for _, e := range cnt.entries {
		if e.id >= 0x1201 && e.id <= 0x1220 {
			if data, ok := try(e); ok {
				return data, nil
			}
		}
	}
	return nil, errors.New("package icon not found")
}

func parseContainer(r io.ReaderAt, size int64) (cntImage, error) {
	if r == nil || size < cntHeaderSize {
		return cntImage{}, errors.New("file too small for CNT header")
	}
	first, err := readAt(r, 0, 0x60, size)
	if err != nil {
		return cntImage{}, err
	}

	var cntBase int64
	switch string(first[:4]) {
	case "\x7fCNT":
		cntBase = 0
	case "\x7fFIH":
		embedded := binary.LittleEndian.Uint64(first[0x58:0x60])
		if embedded == 0 || embedded > uint64(size-cntHeaderSize) {
			return cntImage{}, errors.New("invalid embedded CNT offset")
		}
		cntBase = int64(embedded)
	default:
		return cntImage{}, fmt.Errorf("unsupported package magic %x", first[:4])
	}

	header, err := readAt(r, cntBase, cntHeaderSize, size)
	if err != nil {
		return cntImage{}, err
	}
	if string(header[:4]) != "\x7fCNT" {
		return cntImage{}, errors.New("embedded CNT header not found")
	}

	count := binary.BigEndian.Uint32(header[0x10:0x14])
	tableOff := binary.BigEndian.Uint32(header[0x18:0x1c])
	if count == 0 || count > maxEntries {
		return cntImage{}, fmt.Errorf("invalid CNT entry count %d", count)
	}
	tableSize := int64(count) * entrySize
	table, err := readAt(r, cntBase+int64(tableOff), tableSize, size)
	if err != nil {
		return cntImage{}, fmt.Errorf("read CNT entry table: %w", err)
	}

	entries := make([]entry, 0, count)
	for i := uint32(0); i < count; i++ {
		o := int(i) * entrySize
		entries = append(entries, entry{
			id:       binary.BigEndian.Uint32(table[o : o+4]),
			flags:    binary.BigEndian.Uint32(table[o+8 : o+12]),
			dataOff:  binary.BigEndian.Uint32(table[o+0x10 : o+0x14]),
			dataSize: binary.BigEndian.Uint32(table[o+0x14 : o+0x18]),
		})
	}
	return cntImage{base: cntBase, header: header, entries: entries}, nil
}

func isPNG(data []byte) bool {
	return len(data) >= 8 && bytes.Equal(data[:8], []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'})
}

func applyParamJSON(meta *Metadata, root map[string]json.RawMessage) {
	if v := jsonString(root, "contentId"); v != "" {
		meta.ContentID = v
	}
	if v := jsonString(root, "titleId"); v != "" {
		meta.TitleID = v
	}
	meta.Version = jsonString(root, "contentVersion")
	meta.MasterVersion = jsonString(root, "masterVersion")
	meta.TargetVersion = jsonString(root, "targetContentVersion")

	if raw, ok := root["applicationCategoryType"]; ok {
		var value uint32
		if json.Unmarshal(raw, &value) == nil {
			meta.ApplicationCategoryType = &value
		}
	}

	if raw, ok := root["localizedParameters"]; ok {
		var localized map[string]json.RawMessage
		if json.Unmarshal(raw, &localized) == nil {
			lang := ""
			if v, ok := localized["defaultLanguage"]; ok {
				_ = json.Unmarshal(v, &lang)
			}
			if lang != "" {
				meta.Title = localizedTitle(localized[lang])
			}
			if meta.Title == "" {
				keys := make([]string, 0, len(localized))
				for key := range localized {
					if key != "defaultLanguage" {
						keys = append(keys, key)
					}
				}
				sort.Strings(keys)
				for _, key := range keys {
					if title := localizedTitle(localized[key]); title != "" {
						meta.Title = title
						break
					}
				}
			}
		}
	}

	if meta.TitleID == "" {
		meta.TitleID = titleIDFromContentID(meta.ContentID)
	}
}

func localizedTitle(raw json.RawMessage) string {
	var value struct {
		TitleName string `json:"titleName"`
	}
	if json.Unmarshal(raw, &value) != nil {
		return ""
	}
	return strings.TrimSpace(value.TitleName)
}

func jsonString(root map[string]json.RawMessage, key string) string {
	raw, ok := root[key]
	if !ok {
		return ""
	}
	var value string
	if json.Unmarshal(raw, &value) != nil {
		return ""
	}
	return strings.TrimSpace(value)
}

func classify(meta *Metadata, filename string, patchByStructure bool) {
	switch {
	case patchByStructure || meta.TargetVersion != "":
		meta.PackageType = "patch"
		meta.PackageTypeSource = "structure"
	case looksLikeDLC(meta.Title, meta.ContentID, filename):
		meta.PackageType = "dlc"
		meta.PackageTypeSource = "heuristic"
	case meta.ApplicationCategoryType != nil && *meta.ApplicationCategoryType == 0:
		meta.PackageType = "game"
		meta.PackageTypeSource = "param"
	case meta.ApplicationCategoryType != nil:
		meta.PackageType = "app"
		meta.PackageTypeSource = "param"
	default:
		meta.PackageType = "unknown"
		meta.PackageTypeSource = "none"
	}
}

func looksLikeDLC(title, contentID, filename string) bool {
	value := strings.ToLower(strings.Join([]string{title, contentID, filename}, "\n"))
	for _, marker := range []string{"dlc", "add-on", "addon", "expansion", "season pass"} {
		if strings.Contains(value, marker) {
			return true
		}
	}
	return false
}

func titleIDFromContentID(contentID string) string {
	parts := strings.Split(contentID, "-")
	middle := contentID
	if len(parts) >= 2 {
		middle = parts[1]
	}
	if i := strings.IndexByte(middle, '_'); i > 0 {
		middle = middle[:i]
	}
	if len(middle) < 4 || len(middle) > 16 {
		return ""
	}
	return middle
}

func findReadableEntry(entries []entry, id uint32) (entry, bool) {
	for _, e := range entries {
		if e.id == id && e.flags&0x80000000 == 0 && e.dataSize > 0 {
			return e, true
		}
	}
	return entry{}, false
}

func hasReadableEntry(entries []entry, id uint32) bool {
	_, ok := findReadableEntry(entries, id)
	return ok
}

func readAt(r io.ReaderAt, offset, length, size int64) ([]byte, error) {
	if offset < 0 || length < 0 || offset > size || length > size-offset {
		return nil, io.ErrUnexpectedEOF
	}
	buf := make([]byte, int(length))
	n, err := r.ReadAt(buf, offset)
	if err != nil && !errors.Is(err, io.EOF) {
		return nil, err
	}
	if n != len(buf) {
		return nil, io.ErrUnexpectedEOF
	}
	return buf, nil
}

func readASCII(b []byte) string {
	if i := bytesIndexByte(b, 0); i >= 0 {
		b = b[:i]
	}
	return strings.TrimSpace(string(b))
}

func bytesIndexByte(b []byte, needle byte) int {
	for i, v := range b {
		if v == needle {
			return i
		}
	}
	return -1
}
