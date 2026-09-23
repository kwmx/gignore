package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestLoadCascade(t *testing.T) {
	root := t.TempDir()
	os.Mkdir(filepath.Join(root, ".git"), 0o755)
	sub := filepath.Join(root, "a", "b")
	os.MkdirAll(sub, 0o755)
	user := filepath.Join(t.TempDir(), "config.toml")
	os.WriteFile(user, []byte("[online]\ncache_ttl = \"1h\"\n[presets]\nweb = [\"node\"]\n[generate]\nalways = [\"macos\"]\n"), 0o644)
	os.WriteFile(filepath.Join(root, ProjectFile), []byte("[generate]\ntemplates = [\"go\"]\n[presets]\ncli = [\"go\"]\n"), 0o644)
	t.Setenv("GIGNORE_OFFLINE", "1")
	t.Setenv("GIGNORE_THEME", "light")

	c, err := Load(user, sub)
	if err != nil {
		t.Fatal(err)
	}
	if c.Online.CacheTTL.Duration != time.Hour || c.Online.Enabled || c.TUI.Theme != "light" {
		t.Fatalf("online=%+v theme=%s", c.Online, c.TUI.Theme)
	}
	if strings.Join(c.Generate.Templates, ",") != "go" || strings.Join(c.Generate.Always, ",") != "macos" {
		t.Fatalf("generate = %+v", c.Generate)
	}
	if len(c.Presets) != 2 {
		t.Fatalf("presets should merge: %v", c.Presets)
	}
	if len(c.Sources) != 2 {
		t.Fatalf("sources = %v", c.Sources)
	}
}

func TestProjectFileStopsAtRepoRoot(t *testing.T) {
	outer := t.TempDir()
	os.WriteFile(filepath.Join(outer, ProjectFile), []byte(""), 0o644)
	repo := filepath.Join(outer, "repo")
	os.MkdirAll(filepath.Join(repo, ".git"), 0o755)
	if p := FindProject(repo); p != "" {
		t.Fatalf("found %s outside the repository", p)
	}
}

func TestRejectsUnknownAndInvalid(t *testing.T) {
	f := filepath.Join(t.TempDir(), "c.toml")
	os.WriteFile(f, []byte("[tui]\nthemee = \"dark\"\n"), 0o644)
	if _, err := Load(f, ""); err == nil || !strings.Contains(err.Error(), "tui.themee") {
		t.Fatalf("err = %v", err)
	}
	os.WriteFile(f, []byte("[online]\nprefer = \"cloud\"\n"), 0o644)
	if _, err := Load(f, ""); err == nil {
		t.Fatal("expected invalid prefer error")
	}
	os.WriteFile(f, []byte("[online]\ntimeout = \"soon\"\n"), 0o644)
	if _, err := Load(f, ""); err == nil {
		t.Fatal("expected invalid duration error")
	}
}

func TestStarterMatchesDefaults(t *testing.T) {
	f := filepath.Join(t.TempDir(), "c.toml")
	os.WriteFile(f, []byte(Starter), 0o644)
	c, err := Load(f, "")
	if err != nil {
		t.Fatal(err)
	}
	d := Default()
	if c.Online != d.Online || c.TUI != d.TUI || c.Generate.Dedupe != d.Generate.Dedupe || c.Generate.DetectDepth != d.Generate.DetectDepth {
		t.Fatalf("starter config drifted from defaults:\n%+v\n%+v", c, d)
	}
}
