package catalog

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/kwmx/gignore/internal/version"
)

// DefaultAPI is the gitignore.io endpoint.
const DefaultAPI = "https://www.toptal.com/developers/gitignore/api"

// OnlineOptions controls fetching and caching of the gitignore.io catalog.
type OnlineOptions struct {
	URL      string        // API base URL; the catalog lives at URL + "/list?format=json"
	CacheDir string        // where catalog.json is stored
	TTL      time.Duration // how long a cached catalog counts as fresh
	Timeout  time.Duration // per-request timeout
	Retries  int           // extra attempts after the first failure
	Refresh  bool          // ignore a fresh cache and fetch anyway
	Offline  bool          // never touch the network
	Client   *http.Client  // optional, for tests
}

// OnlineResult describes where the online catalog came from.
type OnlineResult struct {
	Templates []Template
	FetchedAt time.Time
	FromCache bool
	Stale     bool  // a cached copy was used because fetching failed or was disabled
	FetchErr  error // set when a fetch was attempted and failed
}

type cacheFile struct {
	URL       string              `json:"url"`
	FetchedAt time.Time           `json:"fetched_at"`
	Entries   map[string]apiEntry `json:"entries"`
}

type apiEntry struct {
	Key      string `json:"key"`
	Name     string `json:"name"`
	FileName string `json:"fileName"`
	Contents string `json:"contents"`
}

// CachePath returns the catalog cache file for dir.
func CachePath(dir string) string { return filepath.Join(dir, "catalog.json") }

// LoadOnline returns the gitignore.io catalog, from cache when fresh, from the
// network otherwise. A stale cache is used as a fallback when the network fails,
// so the result has templates whenever any copy exists.
func LoadOnline(ctx context.Context, o OnlineOptions) (OnlineResult, error) {
	if o.URL == "" {
		o.URL = DefaultAPI
	}
	o.URL = strings.TrimRight(o.URL, "/")
	cached, cacheErr := readCache(CachePath(o.CacheDir))
	if cacheErr == nil && cached.URL != o.URL {
		cacheErr = errors.New("cache is for a different API URL")
	}
	fresh := cacheErr == nil && time.Since(cached.FetchedAt) < o.TTL
	if fresh && !o.Refresh {
		return OnlineResult{Templates: toTemplates(cached.Entries), FetchedAt: cached.FetchedAt, FromCache: true}, nil
	}
	if o.Offline {
		if cacheErr == nil {
			return OnlineResult{Templates: toTemplates(cached.Entries), FetchedAt: cached.FetchedAt, FromCache: true, Stale: !fresh}, nil
		}
		return OnlineResult{}, errors.New("offline mode and no cached online catalog")
	}
	entries, err := fetch(ctx, o)
	if err != nil {
		if cacheErr == nil {
			return OnlineResult{Templates: toTemplates(cached.Entries), FetchedAt: cached.FetchedAt, FromCache: true, Stale: true, FetchErr: err}, nil
		}
		return OnlineResult{FetchErr: err}, err
	}
	now := time.Now().UTC()
	// A failed cache write only costs a refetch next time, so it isn't fatal.
	_ = writeCache(CachePath(o.CacheDir), cacheFile{URL: o.URL, FetchedAt: now, Entries: entries})
	return OnlineResult{Templates: toTemplates(entries), FetchedAt: now}, nil
}

// Ping checks that the API answers, returning the round-trip time.
func Ping(ctx context.Context, o OnlineOptions) (time.Duration, error) {
	url := strings.TrimRight(orDefault(o.URL, DefaultAPI), "/") + "/list"
	start := time.Now()
	body, err := get(ctx, o, url)
	if err != nil {
		return 0, err
	}
	if !strings.Contains(string(body), "go") {
		return 0, errors.New("unexpected response from template API")
	}
	return time.Since(start), nil
}

func fetch(ctx context.Context, o OnlineOptions) (map[string]apiEntry, error) {
	body, err := get(ctx, o, o.URL+"/list?format=json")
	if err != nil {
		return nil, err
	}
	var entries map[string]apiEntry
	if err := json.Unmarshal(body, &entries); err != nil {
		return nil, fmt.Errorf("decode template catalog: %w", err)
	}
	if len(entries) == 0 {
		return nil, errors.New("template API returned an empty catalog")
	}
	return entries, nil
}

func get(ctx context.Context, o OnlineOptions, url string) ([]byte, error) {
	client := o.Client
	if client == nil {
		client = &http.Client{}
	}
	timeout := o.Timeout
	if timeout <= 0 {
		timeout = 15 * time.Second
	}
	var lastErr error
	for attempt := 0; attempt <= max(o.Retries, 0); attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(time.Duration(attempt*attempt) * 400 * time.Millisecond):
			}
		}
		body, retry, err := getOnce(ctx, client, url, timeout)
		if err == nil {
			return body, nil
		}
		lastErr = err
		if !retry {
			break
		}
	}
	return nil, lastErr
}

func getOnce(ctx context.Context, client *http.Client, url string, timeout time.Duration) ([]byte, bool, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, false, err
	}
	req.Header.Set("User-Agent", version.UserAgent())
	req.Header.Set("Accept", "application/json, text/plain")
	resp, err := client.Do(req)
	if err != nil {
		return nil, true, fmt.Errorf("fetch %s: %w", url, err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 32<<20))
	if err != nil {
		return nil, true, fmt.Errorf("read %s: %w", url, err)
	}
	if resp.StatusCode != http.StatusOK {
		retry := resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500
		return nil, retry, fmt.Errorf("fetch %s: HTTP %d", url, resp.StatusCode)
	}
	return body, false, nil
}

var apiHeader = regexp.MustCompile(`^\s*###\s*[^#\n]+?\s*###\s*\n`)

func toTemplates(entries map[string]apiEntry) []Template {
	out := make([]Template, 0, len(entries))
	for k, e := range entries {
		name := e.Name
		if name == "" {
			name = k
		}
		// gitignore.io prefixes each template with "### Name ###"; gignore adds
		// its own section headers, so drop the upstream one.
		content := apiHeader.ReplaceAllString(strings.TrimLeft(e.Contents, "\n"), "")
		out = append(out, Template{
			Key:     Normalize(k),
			Name:    name,
			Group:   "online",
			Source:  SourceOnline,
			Content: content,
		})
	}
	return out
}

func readCache(path string) (cacheFile, error) {
	var c cacheFile
	b, err := os.ReadFile(path)
	if err != nil {
		return c, err
	}
	if err := json.Unmarshal(b, &c); err != nil {
		return c, err
	}
	if len(c.Entries) == 0 {
		return c, errors.New("empty cache")
	}
	return c, nil
}

// writeCache writes atomically so concurrent runs never see a partial file.
func writeCache(path string, c cacheFile) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	b, err := json.Marshal(c)
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".catalog-*.json")
	if err != nil {
		return err
	}
	if _, err := tmp.Write(b); err != nil {
		tmp.Close()
		os.Remove(tmp.Name())
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmp.Name())
		return err
	}
	return os.Rename(tmp.Name(), path)
}

// CacheInfo summarizes the on-disk online catalog.
type CacheInfo struct {
	Path      string
	Exists    bool
	URL       string
	FetchedAt time.Time
	Templates int
	Bytes     int64
}

// InspectCache reports what is cached in dir without fetching.
func InspectCache(dir string) CacheInfo {
	info := CacheInfo{Path: CachePath(dir)}
	st, err := os.Stat(info.Path)
	if err != nil {
		return info
	}
	info.Exists = true
	info.Bytes = st.Size()
	if c, err := readCache(info.Path); err == nil {
		info.URL, info.FetchedAt, info.Templates = c.URL, c.FetchedAt, len(c.Entries)
	}
	return info
}

func orDefault(s, d string) string {
	if s == "" {
		return d
	}
	return s
}
