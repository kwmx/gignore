// Package usage remembers which templates the user picks, to rank them first.
package usage

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"time"
)

// Entry counts how often and how recently a template was chosen.
type Entry struct {
	Count    int       `json:"count"`
	LastUsed time.Time `json:"last_used"`
}

// Store is the usage history, backed by a JSON file.
type Store struct {
	path    string
	Entries map[string]Entry
}

// Open loads the store at path. A missing or unreadable file yields an empty store.
func Open(path string) *Store {
	s := &Store{path: path, Entries: map[string]Entry{}}
	if b, err := os.ReadFile(path); err == nil {
		_ = json.Unmarshal(b, &s.Entries)
	}
	if s.Entries == nil {
		s.Entries = map[string]Entry{}
	}
	return s
}

// Path returns the default location inside dir.
func Path(dir string) string { return filepath.Join(dir, "usage.json") }

// Record counts one use of each key and saves.
func (s *Store) Record(keys ...string) error {
	now := time.Now().UTC()
	for _, k := range keys {
		e := s.Entries[k]
		e.Count++
		e.LastUsed = now
		s.Entries[k] = e
	}
	return s.save()
}

// Top returns up to n keys, most used first, ties broken by recency.
func (s *Store) Top(n int) []string {
	keys := make([]string, 0, len(s.Entries))
	for k := range s.Entries {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		a, b := s.Entries[keys[i]], s.Entries[keys[j]]
		if a.Count != b.Count {
			return a.Count > b.Count
		}
		if !a.LastUsed.Equal(b.LastUsed) {
			return a.LastUsed.After(b.LastUsed)
		}
		return keys[i] < keys[j]
	})
	if len(keys) > n {
		keys = keys[:n]
	}
	return keys
}

func (s *Store) save() error {
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(s.Entries, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}
