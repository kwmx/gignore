package catalog

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestBuiltinLoadsAndPrefersTopLevel(t *testing.T) {
	ts := Builtin()
	if len(ts) < 250 {
		t.Fatalf("only %d built-in templates", len(ts))
	}
	c := New(Options{}, ts)
	for _, k := range []string{"go", "python", "node", "macos", "visualstudiocode", "jetbrains", "c++"} {
		if _, ok := c.Get(k); !ok {
			t.Errorf("missing built-in %q", k)
		}
	}
	// "Racket" exists at the top level and in community/; the top-level one wins.
	if r, ok := c.Get("racket"); ok && r.Group != "core" {
		t.Errorf("racket came from %s, want core", r.Group)
	}
}

func TestResolveAliasesPresetsAndErrors(t *testing.T) {
	c := New(Options{Presets: map[string][]string{"web": {"node", "macos"}}, Aliases: map[string]string{"k": "go"}}, Builtin())
	ts, err := c.Resolve([]string{"js,golang", "@web", "K", "Go.gitignore"})
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(Keys(ts), ","); got != "node,go,macos" {
		t.Fatalf("resolved %s", got)
	}
	_, err = c.Resolve([]string{"pyhton", "@nope"})
	var ue *UnknownError
	if !errors.As(err, &ue) || len(ue.Names) != 2 {
		t.Fatalf("err = %v", err)
	}
	if !strings.Contains(err.Error(), "did you mean python") {
		t.Fatalf("no suggestion in %q", err)
	}
}

func TestLaterSetsWin(t *testing.T) {
	c := New(Options{}, []Template{{Key: "go", Name: "Go", Content: "a"}}, []Template{{Key: "go", Name: "Go", Content: "b"}})
	if g, _ := c.Get("go"); g.Content != "b" {
		t.Fatalf("content = %q", g.Content)
	}
}

func TestLoadLocal(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "Company.gitignore"), []byte("secret/\n"), 0o644)
	os.WriteFile(filepath.Join(dir, "notes.txt"), []byte("x"), 0o644)
	ts, err := LoadLocal(dir)
	if err != nil || len(ts) != 1 || ts[0].Key != "company" || ts[0].Source != SourceLocal {
		t.Fatalf("got %+v, %v", ts, err)
	}
	if ts, err := LoadLocal(filepath.Join(dir, "missing")); err != nil || ts != nil {
		t.Fatalf("missing dir: %v %v", ts, err)
	}
}

const apiJSON = `{"go":{"key":"go","name":"Go","fileName":"Go.gitignore","contents":"\n### Go ###\n*.exe\n"},
"zig":{"key":"zig","name":"Zig","fileName":"Zig.gitignore","contents":"\n### Zig ###\nzig-cache/\n"}}`

func TestLoadOnlineCachesAndFallsBack(t *testing.T) {
	var hits atomic.Int32
	fail := atomic.Bool{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		if fail.Load() {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		if !strings.HasPrefix(r.Header.Get("User-Agent"), "gignore/") {
			t.Errorf("user agent = %q", r.Header.Get("User-Agent"))
		}
		w.Write([]byte(apiJSON))
	}))
	defer srv.Close()
	o := OnlineOptions{URL: srv.URL, CacheDir: t.TempDir(), TTL: time.Hour, Timeout: time.Second}

	res, err := LoadOnline(context.Background(), o)
	if err != nil || len(res.Templates) != 2 || res.FromCache {
		t.Fatalf("first load: %+v %v", res, err)
	}
	for _, tm := range res.Templates {
		if strings.Contains(tm.Content, "###") {
			t.Errorf("upstream header not stripped: %q", tm.Content)
		}
	}
	res, err = LoadOnline(context.Background(), o)
	if err != nil || !res.FromCache || hits.Load() != 1 {
		t.Fatalf("second load should use cache: %+v hits=%d", res, hits.Load())
	}

	fail.Store(true)
	o.Refresh, o.Retries = true, 1
	res, err = LoadOnline(context.Background(), o)
	if err != nil || !res.Stale || res.FetchErr == nil || len(res.Templates) != 2 {
		t.Fatalf("fallback: %+v %v", res, err)
	}
	if hits.Load() != 3 { // 1 success + 2 attempts (first try and one retry)
		t.Fatalf("hits = %d, want 3", hits.Load())
	}

	o.Offline, o.CacheDir = true, t.TempDir()
	if _, err := LoadOnline(context.Background(), o); err == nil {
		t.Fatal("offline with no cache should fail")
	}
}

func TestSuggest(t *testing.T) {
	c := New(Options{}, Builtin())
	if s := c.Suggest("pyton", 3); len(s) == 0 || s[0] != "python" {
		t.Fatalf("suggest = %v", s)
	}
}
