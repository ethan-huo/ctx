package cache

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func setupTestDir(t *testing.T) {
	t.Helper()
	orig := Dir
	tmp := t.TempDir()
	Dir = func() string { return tmp }
	t.Cleanup(func() { Dir = orig })
}

func TestStoreAndLookup(t *testing.T) {
	setupTestDir(t)

	url := "https://example.com/doc.md"
	content := "# Hello\n\nWorld\n"

	if err := Store(url, content, "http"); err != nil {
		t.Fatal(err)
	}

	got, meta, ok := Lookup(url)
	if !ok {
		t.Fatal("Lookup returned false after Store")
	}
	if got != content {
		t.Errorf("content = %q, want %q", got, content)
	}
	if meta.URL != url {
		t.Errorf("meta.URL = %q, want %q", meta.URL, url)
	}
	if meta.Source != "http" {
		t.Errorf("meta.Source = %q, want %q", meta.Source, "http")
	}
	if meta.Lines != 3 {
		t.Errorf("meta.Lines = %d, want 3", meta.Lines)
	}
	if meta.Size != len(content) {
		t.Errorf("meta.Size = %d, want %d", meta.Size, len(content))
	}
}

func TestLookupMiss(t *testing.T) {
	setupTestDir(t)

	_, _, ok := Lookup("https://no-such-url.example.com")
	if ok {
		t.Error("Lookup returned true for non-existent URL")
	}
}

func TestLookupExpiredTTL(t *testing.T) {
	setupTestDir(t)

	url := "https://example.com/old.md"
	if err := Store(url, "old content", "http"); err != nil {
		t.Fatal(err)
	}

	// Backdate the meta file
	mp := metaPath(url)
	data, _ := os.ReadFile(mp)
	var meta Meta
	json.Unmarshal(data, &meta)
	meta.FetchedAt = time.Now().Add(-2 * time.Hour)
	data, _ = json.MarshalIndent(meta, "", "  ")
	os.WriteFile(mp, data, 0o644)

	_, _, ok := Lookup(url)
	if ok {
		t.Error("Lookup returned true for expired entry")
	}
}

func TestReStoreUpdatesEntry(t *testing.T) {
	setupTestDir(t)

	url := "https://example.com/doc.md"
	Store(url, "v1", "http")
	Store(url, "v2", "http")

	got, _, ok := Lookup(url)
	if !ok {
		t.Fatal("Lookup returned false")
	}
	if got != "v2" {
		t.Errorf("content = %q, want %q", got, "v2")
	}

	// State should have exactly 1 entry (not 2)
	s := loadState()
	if len(s.Entries) != 1 {
		t.Errorf("state has %d entries, want 1", len(s.Entries))
	}
}

func TestEvictionAtMaxEntries(t *testing.T) {
	setupTestDir(t)

	// Store maxEntries + 5 items, oldest should be evicted
	for i := 0; i < maxEntries+5; i++ {
		url := fmt.Sprintf("https://example.com/%d", i)
		if err := Store(url, "content", "test"); err != nil {
			t.Fatal(err)
		}
	}

	s := loadState()
	if len(s.Entries) != maxEntries {
		t.Errorf("state has %d entries after eviction, want %d", len(s.Entries), maxEntries)
	}

	// First 5 entries should have been evicted — their files removed
	for i := 0; i < 5; i++ {
		url := fmt.Sprintf("https://example.com/%d", i)
		if _, err := os.Stat(ContentPath(url)); !os.IsNotExist(err) {
			t.Errorf("evicted file still exists for url index %d", i)
		}
	}

	// Last entry should still exist
	lastURL := fmt.Sprintf("https://example.com/%d", maxEntries+4)
	if _, err := os.Stat(ContentPath(lastURL)); err != nil {
		t.Errorf("latest entry file missing: %v", err)
	}
}

func TestStateFileConsistency(t *testing.T) {
	setupTestDir(t)

	urls := []string{
		"https://a.com/1",
		"https://b.com/2",
		"https://c.com/3",
	}
	for _, u := range urls {
		Store(u, "content-"+u, "test")
	}

	s := loadState()
	if len(s.Entries) != 3 {
		t.Fatalf("state has %d entries, want 3", len(s.Entries))
	}

	// Each entry key should correspond to a real file pair
	for _, e := range s.Entries {
		mdPath := filepath.Join(Dir(), e.Key+".md")
		metaP := filepath.Join(Dir(), e.Key+".meta.json")
		if _, err := os.Stat(mdPath); err != nil {
			t.Errorf("missing .md for key %s: %v", e.Key, err)
		}
		if _, err := os.Stat(metaP); err != nil {
			t.Errorf("missing .meta.json for key %s: %v", e.Key, err)
		}
	}
}

func TestContentPathDeterministic(t *testing.T) {
	p1 := ContentPath("https://example.com/doc")
	p2 := ContentPath("https://example.com/doc")
	if p1 != p2 {
		t.Errorf("ContentPath not deterministic: %q != %q", p1, p2)
	}

	p3 := ContentPath("https://example.com/other")
	if p1 == p3 {
		t.Error("different URLs produced same ContentPath")
	}
}
