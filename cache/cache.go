package cache

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"
)

const (
	defaultTTL = 1 * time.Hour
	maxEntries = 100
)

type Meta struct {
	URL         string    `json:"url"`
	Source      string    `json:"source"`
	ContentType string    `json:"content_type,omitempty"`
	FetchedAt   time.Time `json:"fetched_at"`
	Size        int       `json:"size"`
}

type stateEntry struct {
	Key       string    `json:"key"`
	Ext       string    `json:"ext"`
	URL       string    `json:"url"`
	FetchedAt time.Time `json:"fetched_at"`
}

type stateFile struct {
	Entries []stateEntry `json:"entries"`
}

// Dir returns the cache directory. Overridable for testing.
var Dir = func() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".cache", "ctx")
}

func statePath() string {
	return filepath.Join(Dir(), "state.json")
}

// Key builds a cache key from one or more parts (e.g., operation + URL + params).
func Key(parts ...string) string {
	h := sha256.Sum256([]byte(join(parts)))
	return fmt.Sprintf("%x", h)
}

func join(parts []string) string {
	if len(parts) == 1 {
		return parts[0]
	}
	total := 0
	for _, p := range parts {
		total += len(p) + 1
	}
	buf := make([]byte, 0, total)
	for i, p := range parts {
		if i > 0 {
			buf = append(buf, 0)
		}
		buf = append(buf, p...)
	}
	return string(buf)
}

// Path returns the content file path for a key with the given extension.
func Path(key, ext string) string {
	return filepath.Join(Dir(), key+ext)
}

func metaPath(key string) string {
	return filepath.Join(Dir(), key+".meta.json")
}

// Lookup retrieves cached data by key and expected extension.
func Lookup(key, ext string) (data []byte, meta Meta, ok bool) {
	mp := metaPath(key)
	raw, err := os.ReadFile(mp)
	if err != nil {
		return nil, Meta{}, false
	}
	if err := json.Unmarshal(raw, &meta); err != nil {
		return nil, Meta{}, false
	}
	if time.Since(meta.FetchedAt) > defaultTTL {
		return nil, Meta{}, false
	}
	data, err = os.ReadFile(Path(key, ext))
	if err != nil {
		return nil, Meta{}, false
	}
	return data, meta, true
}

// Store saves data to cache with the given key, extension, and metadata.
func Store(key string, data []byte, ext string, meta Meta) error {
	d := Dir()
	if err := os.MkdirAll(d, 0o755); err != nil {
		return err
	}

	meta.Size = len(data)
	if meta.FetchedAt.IsZero() {
		meta.FetchedAt = time.Now()
	}

	metaJSON, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(Path(key, ext), data, 0o644); err != nil {
		return err
	}
	if err := os.WriteFile(metaPath(key), metaJSON, 0o644); err != nil {
		return err
	}

	return updateState(key, ext, meta.URL, meta.FetchedAt)
}

func loadState() stateFile {
	data, err := os.ReadFile(statePath())
	if err != nil {
		return stateFile{}
	}
	var s stateFile
	if err := json.Unmarshal(data, &s); err != nil {
		return stateFile{}
	}
	return s
}

func saveState(s stateFile) error {
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(statePath(), data, 0o644)
}

func updateState(key, ext, url string, fetchedAt time.Time) error {
	s := loadState()

	filtered := make([]stateEntry, 0, len(s.Entries))
	for _, e := range s.Entries {
		if e.Key != key {
			filtered = append(filtered, e)
		}
	}

	filtered = append(filtered, stateEntry{
		Key:       key,
		Ext:       ext,
		URL:       url,
		FetchedAt: fetchedAt,
	})

	if len(filtered) > maxEntries {
		sort.Slice(filtered, func(i, j int) bool {
			return filtered[i].FetchedAt.Before(filtered[j].FetchedAt)
		})
		evict := filtered[:len(filtered)-maxEntries]
		filtered = filtered[len(filtered)-maxEntries:]
		for _, e := range evict {
			removeFiles(e.Key, e.Ext)
		}
	}

	s.Entries = filtered
	return saveState(s)
}

func removeFiles(key, ext string) {
	d := Dir()
	if ext != "" {
		os.Remove(filepath.Join(d, key+ext))
	}
	os.Remove(filepath.Join(d, key+".meta.json"))
}
