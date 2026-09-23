package pkgstore

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Loopayeh/pkg-sender/nas/internal/pkgmeta"
)

type Package struct {
	ID             string `json:"id"`
	Name           string `json:"name"`
	RelativePath   string `json:"relativePath"`
	Path           string `json:"path,omitempty"`
	LibraryRoot    string `json:"libraryRoot,omitempty"`
	Size           int64  `json:"size"`
	MetadataParsed bool   `json:"metadataParsed"`
	pkgmeta.Metadata
}

type entry struct {
	Package
	path    string
	modTime time.Time
}

type Store struct {
	root    string
	roots   []string
	aliases TitleAliases

	mu      sync.RWMutex
	entries map[string]entry
	list    []Package
}

func New(root string) (*Store, error) {
	return NewWithTitleAliases(root, nil)
}

func NewWithTitleAliases(root string, aliases TitleAliases) (*Store, error) {
	return NewWithRootsAndTitleAliases([]string{root}, aliases)
}

func NewWithRoots(roots []string) (*Store, error) {
	return NewWithRootsAndTitleAliases(roots, nil)
}

func NewWithRootsAndTitleAliases(roots []string, aliases TitleAliases) (*Store, error) {
	normalized, err := NormalizeRoots(roots)
	if err != nil {
		return nil, err
	}
	return &Store{
		root:    normalized[0],
		roots:   normalized,
		aliases: aliases.normalized(),
		entries: make(map[string]entry),
	}, nil
}

func NormalizeRoots(roots []string) ([]string, error) {
	seen := map[string]bool{}
	normalized := make([]string, 0, len(roots))
	for _, root := range roots {
		root = strings.TrimSpace(root)
		if root == "" {
			continue
		}
		abs, err := filepath.Abs(root)
		if err != nil {
			return nil, err
		}
		abs = filepath.Clean(abs)
		if seen[abs] {
			continue
		}
		info, err := os.Stat(abs)
		if err != nil {
			return nil, fmt.Errorf("package root %q is not accessible: %w", root, err)
		}
		if !info.IsDir() {
			return nil, fmt.Errorf("package root %q is not a directory", root)
		}
		seen[abs] = true
		normalized = append(normalized, abs)
	}
	if len(normalized) == 0 {
		return nil, errors.New("at least one package root is required")
	}
	return normalized, nil
}

func (s *Store) Root() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.root
}

func (s *Store) Roots() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]string, len(s.roots))
	copy(out, s.roots)
	return out
}

func (s *Store) SetRoots(roots []string) error {
	normalized, err := NormalizeRoots(roots)
	if err != nil {
		return err
	}
	s.mu.Lock()
	s.root = normalized[0]
	s.roots = normalized
	s.mu.Unlock()
	return nil
}

func (s *Store) Scan() (int, error) {
	s.mu.RLock()
	roots := make([]string, len(s.roots))
	copy(roots, s.roots)
	aliases := s.aliases
	s.mu.RUnlock()

	next := make(map[string]entry)
	packages := make([]Package, 0)
	for rootIndex, root := range roots {
		if err := scanRoot(rootIndex, root, len(roots), aliases, next, &packages); err != nil {
			return 0, err
		}
	}

	sort.Slice(packages, func(i, j int) bool {
		left, right := strings.ToLower(packages[i].Path), strings.ToLower(packages[j].Path)
		if left != right {
			return left < right
		}
		return packages[i].ID < packages[j].ID
	})

	s.mu.Lock()
	s.entries = next
	s.list = packages
	s.mu.Unlock()

	return len(packages), nil
}

func scanRoot(rootIndex int, root string, rootCount int, aliases TitleAliases, next map[string]entry, packages *[]Package) error {
	return filepath.WalkDir(root, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			// Keep one unreadable subtree from invalidating the whole library.
			if path == root {
				return walkErr
			}
			return nil
		}
		if path == root {
			return nil
		}
		if d.Type()&os.ModeSymlink != 0 {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if d.IsDir() || !strings.EqualFold(filepath.Ext(d.Name()), ".pkg") {
			return nil
		}

		info, err := d.Info()
		if err != nil || !info.Mode().IsRegular() {
			return nil
		}

		rel, err := filepath.Rel(root, path)
		if err != nil || rel == "." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || rel == ".." {
			return nil
		}
		rel = filepath.ToSlash(rel)
		idInput := rel
		if rootCount > 1 && rootIndex > 0 {
			idInput = strconv.Itoa(rootIndex) + "\x00" + rel
		}
		id := packageID(idInput)
		meta, metaErr := pkgmeta.ReadFile(path)
		if metaErr == nil {
			aliases.apply(&meta)
		}
		pkg := Package{
			ID:             id,
			Name:           filepath.Base(path),
			RelativePath:   rel,
			Path:           filepath.ToSlash(filepath.Join(root, rel)),
			LibraryRoot:    filepath.ToSlash(root),
			Size:           info.Size(),
			MetadataParsed: metaErr == nil,
			Metadata:       meta,
		}
		next[id] = entry{
			Package: pkg,
			path:    path,
			modTime: info.ModTime(),
		}
		*packages = append(*packages, pkg)
		return nil
	})
}

func (s *Store) List() []Package {
	s.mu.RLock()
	defer s.mu.RUnlock()

	out := make([]Package, len(s.list))
	copy(out, s.list)
	return out
}

func (s *Store) MissingTitleAliases() []MissingTitleAlias {
	s.mu.RLock()
	defer s.mu.RUnlock()

	byKey := make(map[string]*MissingTitleAlias)
	order := make([]string, 0)
	for _, pkg := range s.list {
		if !pkg.MetadataParsed || strings.TrimSpace(pkg.SecondaryTitle) != "" {
			continue
		}
		key := strings.TrimSpace(pkg.TitleID)
		if key == "" {
			key = strings.TrimSpace(pkg.ContentID)
		}
		if key == "" {
			continue
		}
		key = normalizeAliasKey(key)
		item := byKey[key]
		if item == nil {
			item = &MissingTitleAlias{
				TitleID:            pkg.TitleID,
				ContentID:          pkg.ContentID,
				DisplayTitle:       pkg.DisplayTitle,
				Title:              pkg.Title,
				LocalizedLanguages: localizedLanguageKeys(pkg.LocalizedTitles),
			}
			byKey[key] = item
			order = append(order, key)
		}
		item.PackageCount++
	}
	sort.Strings(order)
	out := make([]MissingTitleAlias, 0, len(order))
	for _, key := range order {
		out = append(out, *byKey[key])
	}
	return out
}

func (s *Store) TitleAliasExport() TitleAliasExport {
	return BuildTitleAliasExport(s.MissingTitleAliases())
}

func (s *Store) SetTitleAliases(aliases TitleAliases) {
	s.mu.Lock()
	s.aliases = aliases.normalized()
	s.mu.Unlock()
}

func (s *Store) Get(id string) (Package, string, time.Time, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	e, ok := s.entries[id]
	if !ok {
		return Package{}, "", time.Time{}, false
	}
	return e.Package, e.path, e.modTime, true
}

func packageID(relativePath string) string {
	sum := sha256.Sum256([]byte(filepath.ToSlash(relativePath)))
	return hex.EncodeToString(sum[:])
}
