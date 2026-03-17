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
	content := []byte("# Hello\n\nWorld\n")
	key := Key("markdown", url)

	err := Store(key, content, ".md", Meta{URL: url, Source: "http"})
	if err != nil {
		t.Fatal(err)
	}

	got, meta, ok := Lookup(key, ".md")
	if !ok {
		t.Fatal("Lookup returned false after Store")
	}
	if string(got) != string(content) {
		t.Errorf("content = %q, want %q", got, content)
	}
	if meta.URL != url {
		t.Errorf("meta.URL = %q, want %q", meta.URL, url)
	}
	if meta.Source != "http" {
		t.Errorf("meta.Source = %q, want %q", meta.Source, "http")
	}
	if meta.Size != len(content) {
		t.Errorf("meta.Size = %d, want %d", meta.Size, len(content))
	}
}

func TestLookupMiss(t *testing.T) {
	setupTestDir(t)

	key := Key("markdown", "https://no-such-url.example.com")
	_, _, ok := Lookup(key, ".md")
	if ok {
		t.Error("Lookup returned true for non-existent key")
	}
}

func TestLookupExpiredTTL(t *testing.T) {
	setupTestDir(t)

	url := "https://example.com/old.md"
	key := Key("markdown", url)
	Store(key, []byte("old content"), ".md", Meta{URL: url, Source: "http"})

	// Backdate the meta file
	mp := metaPath(key)
	data, _ := os.ReadFile(mp)
	var meta Meta
	json.Unmarshal(data, &meta)
	meta.FetchedAt = time.Now().Add(-2 * time.Hour)
	data, _ = json.MarshalIndent(meta, "", "  ")
	os.WriteFile(mp, data, 0o644)

	_, _, ok := Lookup(key, ".md")
	if ok {
		t.Error("Lookup returned true for expired entry")
	}
}

func TestReStoreUpdatesEntry(t *testing.T) {
	setupTestDir(t)

	url := "https://example.com/doc.md"
	key := Key("markdown", url)
	Store(key, []byte("v1"), ".md", Meta{URL: url, Source: "http"})
	Store(key, []byte("v2"), ".md", Meta{URL: url, Source: "http"})

	got, _, ok := Lookup(key, ".md")
	if !ok {
		t.Fatal("Lookup returned false")
	}
	if string(got) != "v2" {
		t.Errorf("content = %q, want %q", got, "v2")
	}

	s := loadState()
	if len(s.Entries) != 1 {
		t.Errorf("state has %d entries, want 1", len(s.Entries))
	}
}

func TestEvictionAtMaxEntries(t *testing.T) {
	setupTestDir(t)

	for i := 0; i < maxEntries+5; i++ {
		url := fmt.Sprintf("https://example.com/%d", i)
		key := Key("markdown", url)
		if err := Store(key, []byte("content"), ".md", Meta{URL: url, Source: "test"}); err != nil {
			t.Fatal(err)
		}
	}

	s := loadState()
	if len(s.Entries) != maxEntries {
		t.Errorf("state has %d entries after eviction, want %d", len(s.Entries), maxEntries)
	}

	// First 5 entries should have been evicted
	for i := 0; i < 5; i++ {
		url := fmt.Sprintf("https://example.com/%d", i)
		key := Key("markdown", url)
		if _, err := os.Stat(Path(key, ".md")); !os.IsNotExist(err) {
			t.Errorf("evicted file still exists for url index %d", i)
		}
	}

	// Last entry should still exist
	lastURL := fmt.Sprintf("https://example.com/%d", maxEntries+4)
	lastKey := Key("markdown", lastURL)
	if _, err := os.Stat(Path(lastKey, ".md")); err != nil {
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
		key := Key("markdown", u)
		Store(key, []byte("content-"+u), ".md", Meta{URL: u, Source: "test"})
	}

	s := loadState()
	if len(s.Entries) != 3 {
		t.Fatalf("state has %d entries, want 3", len(s.Entries))
	}

	for _, e := range s.Entries {
		contentP := filepath.Join(Dir(), e.Key+e.Ext)
		metaP := filepath.Join(Dir(), e.Key+".meta.json")
		if _, err := os.Stat(contentP); err != nil {
			t.Errorf("missing content for key %s: %v", e.Key, err)
		}
		if _, err := os.Stat(metaP); err != nil {
			t.Errorf("missing .meta.json for key %s: %v", e.Key, err)
		}
	}
}

func TestDifferentOpsOnSameURL(t *testing.T) {
	setupTestDir(t)

	url := "https://example.com/page"
	mdKey := Key("markdown", url)
	ssKey := Key("screenshot", url)

	Store(mdKey, []byte("# Hello"), ".md", Meta{URL: url, Source: "cloudflare"})
	Store(ssKey, []byte{0x89, 0x50, 0x4E, 0x47}, ".png", Meta{URL: url, Source: "cloudflare", ContentType: "image/png"})

	md, _, ok := Lookup(mdKey, ".md")
	if !ok || string(md) != "# Hello" {
		t.Error("markdown lookup failed")
	}

	ss, meta, ok := Lookup(ssKey, ".png")
	if !ok || len(ss) != 4 {
		t.Error("screenshot lookup failed")
	}
	if meta.ContentType != "image/png" {
		t.Errorf("content type = %q, want image/png", meta.ContentType)
	}
}

func TestKeyDeterministic(t *testing.T) {
	k1 := Key("markdown", "https://example.com/doc")
	k2 := Key("markdown", "https://example.com/doc")
	if k1 != k2 {
		t.Errorf("Key not deterministic: %q != %q", k1, k2)
	}

	k3 := Key("screenshot", "https://example.com/doc")
	if k1 == k3 {
		t.Error("different operations produced same Key")
	}
}
