package pkgstore

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

type Package struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	RelativePath string `json:"relativePath"`
	Size         int64  `json:"size"`
}

type entry struct {
	Package
	path    string
	modTime time.Time
}

type Store struct {
	root string

	mu      sync.RWMutex
	entries map[string]entry
	list    []Package
}

func New(root string) (*Store, error) {
	if strings.TrimSpace(root) == "" {
		return nil, errors.New("package root is required")
	}

	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	info, err := os.Stat(abs)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		return nil, errors.New("package root is not a directory")
	}

	return &Store{
		root:    filepath.Clean(abs),
		entries: make(map[string]entry),
	}, nil
}

func (s *Store) Root() string {
	return s.root
}

func (s *Store) Scan() (int, error) {
	next := make(map[string]entry)
	packages := make([]Package, 0)

	err := filepath.WalkDir(s.root, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			// Keep one unreadable subtree from invalidating the whole library.
			if path == s.root {
				return walkErr
			}
			return nil
		}
		if path == s.root {
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

		rel, err := filepath.Rel(s.root, path)
		if err != nil || rel == "." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || rel == ".." {
			return nil
		}
		rel = filepath.ToSlash(rel)
		id := packageID(rel)
		pkg := Package{
			ID:           id,
			Name:         filepath.Base(path),
			RelativePath: rel,
			Size:         info.Size(),
		}
		next[id] = entry{
			Package: pkg,
			path:    path,
			modTime: info.ModTime(),
		}
		packages = append(packages, pkg)
		return nil
	})
	if err != nil {
		return 0, err
	}

	sort.Slice(packages, func(i, j int) bool {
		return strings.ToLower(packages[i].RelativePath) < strings.ToLower(packages[j].RelativePath)
	})

	s.mu.Lock()
	s.entries = next
	s.list = packages
	s.mu.Unlock()

	return len(packages), nil
}

func (s *Store) List() []Package {
	s.mu.RLock()
	defer s.mu.RUnlock()

	out := make([]Package, len(s.list))
	copy(out, s.list)
	return out
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
