package detect

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDetect(t *testing.T) {
	dir := t.TempDir()
	for _, f := range []string{"go.mod", "web/package.json", "web/node_modules/x/Cargo.toml", "deep/a/b/c/pom.xml", ".vscode/settings.json", "App.csproj"} {
		p := filepath.Join(dir, f)
		os.MkdirAll(filepath.Dir(p), 0o755)
		os.WriteFile(p, nil, 0o644)
	}
	ms, err := Detect(dir, Options{MaxDepth: 2})
	if err != nil {
		t.Fatal(err)
	}
	got := strings.Join(Templates(ms), ",")
	// rust is inside node_modules (skipped); maven is too deep.
	if got != "go,node,visualstudio,visualstudiocode" {
		t.Fatalf("got %s", got)
	}
	if ms[1].Reasons[0] != "web/package.json" {
		t.Fatalf("reason = %v", ms[1].Reasons)
	}
}

func TestDetectIncludesOS(t *testing.T) {
	ms, err := Detect(t.TempDir(), Options{IncludeOS: true})
	if err != nil {
		t.Fatal(err)
	}
	if OSTemplate() != "" && (len(ms) != 1 || ms[0].Kind != "os") {
		t.Fatalf("got %+v", ms)
	}
}
