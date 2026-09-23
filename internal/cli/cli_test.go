package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kwmx/gignore/internal/gitx"
)

type env struct {
	t   *testing.T
	dir string
}

func newEnv(t *testing.T) *env {
	t.Helper()
	t.Setenv("GIGNORE_CONFIG_DIR", t.TempDir())
	t.Setenv("GIGNORE_CACHE_DIR", t.TempDir())
	t.Setenv("GIGNORE_STATE_DIR", t.TempDir())
	t.Setenv("GIGNORE_OFFLINE", "1")
	t.Setenv("NO_COLOR", "1")
	return &env{t: t, dir: t.TempDir()}
}

func (e *env) run(args ...string) (string, string, int) {
	e.t.Helper()
	var out, errb bytes.Buffer
	code := Execute(context.Background(), append([]string{"-C", e.dir}, args...), strings.NewReader(""), &out, &errb)
	return out.String(), errb.String(), code
}

func (e *env) write(name, body string) {
	e.t.Helper()
	p := filepath.Join(e.dir, name)
	os.MkdirAll(filepath.Dir(p), 0o755)
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		e.t.Fatal(err)
	}
}

func (e *env) read(name string) string {
	e.t.Helper()
	b, err := os.ReadFile(filepath.Join(e.dir, name))
	if err != nil {
		e.t.Fatal(err)
	}
	return string(b)
}

func TestGenerateStdout(t *testing.T) {
	e := newEnv(t)
	out, _, code := e.run("generate", "go", "--raw")
	if code != 0 || !strings.HasPrefix(out, "### Go ###\n") || strings.Contains(out, "gignore start") {
		t.Fatalf("code=%d out=%q", code, out[:min(80, len(out))])
	}
	out, _, _ = e.run("generate", "go")
	if !strings.HasPrefix(out, "# >>> gignore start: go\n") {
		t.Fatalf("block markers missing: %q", out[:40])
	}
}

func TestWriteAddRemoveUpdate(t *testing.T) {
	e := newEnv(t)
	e.write(".gitignore", "mine.txt\n")
	if _, errs, code := e.run("generate", "go", "-w"); code != 0 {
		t.Fatalf("generate: %d %s", code, errs)
	}
	if _, errs, code := e.run("add", "rust", "node"); code != 0 {
		t.Fatalf("add: %d %s", code, errs)
	}
	got := e.read(".gitignore")
	if !strings.HasPrefix(got, "# >>> gignore start: go, rust, node\n") || !strings.HasSuffix(got, "mine.txt\n") {
		t.Fatalf("after add:\n%s", got)
	}
	if _, _, code := e.run("remove", "rust"); code != 0 {
		t.Fatal("remove failed")
	}
	if _, _, code := e.run("update", "--check"); code != 0 {
		t.Fatalf("fresh file reported stale: %d", code)
	}
	e.write(".gitignore", strings.Replace(e.read(".gitignore"), "*.exe\n", "", 1))
	if _, _, code := e.run("update", "--check"); code != ExitFindings {
		t.Fatalf("stale file: code %d, want %d", code, ExitFindings)
	}
	if _, _, code := e.run("update"); code != 0 {
		t.Fatal("update failed")
	}
	if _, _, code := e.run("remove", "--all"); code != 0 || e.read(".gitignore") != "mine.txt\n" {
		t.Fatalf("remove --all left:\n%s", e.read(".gitignore"))
	}
}

func TestDryRunWritesNothing(t *testing.T) {
	e := newEnv(t)
	out, _, code := e.run("generate", "go", "-n")
	if code != 0 || !strings.Contains(out, "+++ b/.gitignore") {
		t.Fatalf("code=%d out=%s", code, out)
	}
	if _, err := os.Stat(filepath.Join(e.dir, ".gitignore")); !os.IsNotExist(err) {
		t.Fatal("dry run created the file")
	}
}

func TestReplaceNeedsConfirmation(t *testing.T) {
	e := newEnv(t)
	e.write(".gitignore", "mine.txt\n")
	if _, errs, code := e.run("generate", "go", "-w", "--replace"); code != ExitFailure || !strings.Contains(errs, "--yes") {
		t.Fatalf("code=%d errs=%s", code, errs)
	}
	if _, _, code := e.run("generate", "go", "-w", "--replace", "-y"); code != 0 || strings.Contains(e.read(".gitignore"), "mine.txt") {
		t.Fatal("replace with --yes should drop user rules")
	}
}

func TestErrorsAndUsage(t *testing.T) {
	e := newEnv(t)
	if _, errs, code := e.run("generate", "pyhton"); code != ExitFailure || !strings.Contains(errs, "did you mean python") {
		t.Fatalf("code=%d errs=%s", code, errs)
	}
	if _, _, code := e.run("generate", "--bogus"); code != ExitUsage {
		t.Fatalf("unknown flag: code %d", code)
	}
	if _, _, code := e.run("generate"); code != ExitUsage {
		t.Fatalf("no templates: code %d", code)
	}
	if _, _, code := e.run("update"); code != ExitFailure {
		t.Fatalf("update without block: code %d", code)
	}
	if out, _, code := e.run("node,macos"); code != 0 || !strings.HasPrefix(out, "# >>> gignore start: node, macos\n") {
		t.Fatalf("piped root command should print rules: code=%d out=%.60q", code, out)
	}
	if _, errs, code := e.run("nodee"); code != ExitFailure || !strings.Contains(errs, "did you mean node") {
		t.Fatalf("piped unknown template: code=%d errs=%s", code, errs)
	}
	if _, errs, code := e.run(); code != ExitUsage || !strings.Contains(errs, "terminal") {
		t.Fatalf("TUI without a terminal: code=%d errs=%s", code, errs)
	}
}

func TestListJSONAndDetect(t *testing.T) {
	e := newEnv(t)
	out, _, code := e.run("list", "pyth", "--json")
	var rows []map[string]any
	if code != 0 || json.Unmarshal([]byte(out), &rows) != nil || rows[0]["key"] != "python" {
		t.Fatalf("code=%d out=%s", code, out)
	}
	e.write("Cargo.toml", "")
	out, _, _ = e.run("detect", "--json")
	if !strings.Contains(out, `"template": "rust"`) {
		t.Fatalf("detect: %s", out)
	}
	if _, errs, code := e.run("generate", "--detect", "-w"); code != 0 || !strings.Contains(e.read(".gitignore"), "### Rust ###") {
		t.Fatalf("generate --detect: %d %s", code, errs)
	}
}

func TestProjectConfigDefaults(t *testing.T) {
	e := newEnv(t)
	e.write(".gignore.toml", "[generate]\ntemplates = [\"@stack\"]\n[presets]\nstack = [\"go\", \"node\"]\n")
	if _, errs, code := e.run("generate", "-w"); code != 0 {
		t.Fatalf("%d %s", code, errs)
	}
	if !strings.HasPrefix(e.read(".gitignore"), "# >>> gignore start: go, node\n") {
		t.Fatal(e.read(".gitignore"))
	}
}

func TestCheckAndExplainInRepo(t *testing.T) {
	if !gitx.Available() {
		t.Skip("git not installed")
	}
	e := newEnv(t)
	git := func(args ...string) {
		cmd := exec.Command("git", args...)
		cmd.Dir = e.dir
		cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@example.com", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@example.com")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	git("init", "-q")
	e.write("app.exe", "")
	git("add", "app.exe")
	git("commit", "-qm", "init")
	e.run("generate", "go", "-w")
	e.write(".gitignore", e.read(".gitignore")+"out/\n!out/keep\n")

	out, _, code := e.run("check", "--json")
	if code != 0 || !strings.Contains(out, "tracked-ignored") || !strings.Contains(out, "unreachable-negation") {
		t.Fatalf("check: %d %s", code, out)
	}
	if _, _, code := e.run("check", "--strict"); code != ExitFindings {
		t.Fatalf("check --strict: code %d", code)
	}
	out, _, _ = e.run("explain", "app.exe", "src/main.go")
	if !strings.Contains(out, "(from template Go)") || !strings.Contains(out, "not ignored src/main.go") {
		t.Fatalf("explain: %s", out)
	}
	if _, _, code := e.run("untrack", "-y"); code != 0 {
		t.Fatal("untrack failed")
	}
	out, _, _ = e.run("check", "--json")
	if strings.Contains(out, "tracked-ignored") {
		t.Fatalf("file still tracked after untrack: %s", out)
	}
}
