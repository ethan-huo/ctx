package cache

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const (
	defaultTTL = 1 * time.Hour
	maxEntries = 100
)

type Meta struct {
	URL       string    `json:"url"`
	Source    string    `json:"source"`
	FetchedAt time.Time `json:"fetched_at"`
	Lines    int       `json:"lines"`
	Size     int       `json:"size"`
}

type stateEntry struct {
	Key       string    `json:"key"`
	URL       string    `json:"url"`
	FetchedAt time.Time `json:"fetched_at"`
}

type stateFile struct {
	Entries []stateEntry `json:"entries"`
}

// Dir returns the cache directory. Overridable for testing.
var Dir = func() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".cache", "docs7")
}

func statePath() string {
	return filepath.Join(Dir(), "state.json")
}

func Key(url string) string {
	h := sha256.Sum256([]byte(url))
	return fmt.Sprintf("%x", h)
}

func ContentPath(url string) string {
	return filepath.Join(Dir(), Key(url)+".md")
}

func metaPath(url string) string {
	return filepath.Join(Dir(), Key(url)+".meta.json")
}

func Lookup(url string) (content string, meta Meta, ok bool) {
	mp := metaPath(url)
	data, err := os.ReadFile(mp)
	if err != nil {
		return "", Meta{}, false
	}
	if err := json.Unmarshal(data, &meta); err != nil {
		return "", Meta{}, false
	}
	if time.Since(meta.FetchedAt) > defaultTTL {
		return "", Meta{}, false
	}
	raw, err := os.ReadFile(ContentPath(url))
	if err != nil {
		return "", Meta{}, false
	}
	return string(raw), meta, true
}

func Store(url, content, source string) error {
	d := Dir()
	if err := os.MkdirAll(d, 0o755); err != nil {
		return err
	}

	lines := strings.Count(content, "\n")
	now := time.Now()
	meta := Meta{
		URL:       url,
		Source:    source,
		FetchedAt: now,
		Lines:    lines,
		Size:     len(content),
	}
	metaJSON, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(ContentPath(url), []byte(content), 0o644); err != nil {
		return err
	}
	if err := os.WriteFile(metaPath(url), metaJSON, 0o644); err != nil {
		return err
	}

	// Update state index and evict if over limit
	return updateState(Key(url), url, now)
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

func updateState(key, url string, fetchedAt time.Time) error {
	s := loadState()

	// Remove existing entry with same key (re-fetch case)
	filtered := make([]stateEntry, 0, len(s.Entries))
	for _, e := range s.Entries {
		if e.Key != key {
			filtered = append(filtered, e)
		}
	}

	// Append new entry
	filtered = append(filtered, stateEntry{
		Key:       key,
		URL:       url,
		FetchedAt: fetchedAt,
	})

	// Evict oldest if over limit
	if len(filtered) > maxEntries {
		sort.Slice(filtered, func(i, j int) bool {
			return filtered[i].FetchedAt.Before(filtered[j].FetchedAt)
		})
		evict := filtered[:len(filtered)-maxEntries]
		filtered = filtered[len(filtered)-maxEntries:]
		for _, e := range evict {
			removeFiles(e.Key)
		}
	}

	s.Entries = filtered
	return saveState(s)
}

func removeFiles(key string) {
	d := Dir()
	os.Remove(filepath.Join(d, key+".md"))
	os.Remove(filepath.Join(d, key+".meta.json"))
}
