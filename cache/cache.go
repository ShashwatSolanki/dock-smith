package cache

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"docksmith/store"
)

// Index maps cache keys to layer digests.
type Index struct {
	Entries map[string]string `json:"entries"`
}

var mu sync.Mutex

func indexPath() string {
	return filepath.Join(store.CacheDir(), "index.json")
}

func loadIndex() (*Index, error) {
	data, err := os.ReadFile(indexPath())
	if err != nil {
		if os.IsNotExist(err) {
			return &Index{Entries: make(map[string]string)}, nil
		}
		return nil, fmt.Errorf("read cache index: %w", err)
	}
	var idx Index
	if err := json.Unmarshal(data, &idx); err != nil {
		return &Index{Entries: make(map[string]string)}, nil
	}
	if idx.Entries == nil {
		idx.Entries = make(map[string]string)
	}
	return &idx, nil
}

func saveIndex(idx *Index) error {
	data, err := json.MarshalIndent(idx, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal cache index: %w", err)
	}
	return os.WriteFile(indexPath(), data, 0644)
}

// Lookup checks if a cache key exists and the referenced layer is on disk.
func Lookup(key string) string {
	mu.Lock()
	defer mu.Unlock()

	idx, err := loadIndex()
	if err != nil {
		return ""
	}
