package block

import (
	"errors"
	"strings"
	"testing"
)

func TestApplyNewFile(t *testing.T) {
	blk := Render([]string{"go"}, "*.exe\n")
	got, err := Apply("", blk, Merge)
	if err != nil {
		t.Fatal(err)
	}
	want := "# >>> gignore start: go\n" + noteLine + "\n*.exe\n# <<< gignore end\n\n" + userHeader + "\n"
	if got != want {
		t.Fatalf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestApplyKeepsUserRulesAroundBlock(t *testing.T) {
	existing := "top.txt\n" + Render([]string{"go"}, "*.exe") + "\nbottom.txt\n!keep.exe\n"
	got, err := Apply(existing, Render([]string{"go", "node"}, "*.exe\nnode_modules/"), Merge)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"top.txt\n# >>> gignore start: go, node\n", "node_modules/\n# <<< gignore end\n\nbottom.txt\n!keep.exe\n"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
	if strings.Count(got, "gignore start") != 1 {
		t.Errorf("expected exactly one block:\n%s", got)
	}
}

func TestApplyLegacyFilePutsBlockFirst(t *testing.T) {
	got, err := Apply("secrets.env\n", Render([]string{"go"}, "*.exe"), Merge)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(got, "# >>> gignore start: go") || !strings.HasSuffix(got, userHeader+"\nsecrets.env\n") {
		t.Fatalf("unexpected layout:\n%s", got)
	}
}

func TestApplyIsIdempotent(t *testing.T) {
	blk := Render([]string{"go"}, "*.exe")
	once, _ := Apply("mine.txt\n", blk, Merge)
	twice, _ := Apply(once, blk, Merge)
	if once != twice {
		t.Fatalf("second apply changed the file:\n%s\n---\n%s", once, twice)
	}
}

func TestApplyPreservesCRLF(t *testing.T) {
	got, err := Apply("a.txt\r\nb.txt\r\n", Render([]string{"go"}, "*.exe"), Merge)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(got, "\n") != strings.Count(got, "\r\n") {
		t.Fatalf("mixed line endings: %q", got)
	}
}

func TestMalformed(t *testing.T) {
	_, err := Apply("# >>> gignore start: go\n*.exe\n", Render([]string{"go"}, "*.exe"), Merge)
	if !errors.Is(err, ErrMalformed) {
		t.Fatalf("err = %v, want ErrMalformed", err)
	}
	got, err := Apply("# >>> gignore start: go\n*.exe\n", Render([]string{"go"}, "*.exe"), Replace)
	if err != nil || strings.Count(got, "gignore start") != 1 {
		t.Fatalf("replace should recover: err=%v\n%s", err, got)
	}
}

func TestParseTemplates(t *testing.T) {
	d, err := Parse(Render([]string{"go", "c++", "visualstudiocode"}, "x"))
	if err != nil || !d.Found {
		t.Fatal(err)
	}
	if strings.Join(d.Templates, " ") != "go c++ visualstudiocode" {
		t.Fatalf("templates = %v", d.Templates)
	}
	if d.Body != "x" {
		t.Fatalf("body = %q", d.Body)
	}
}

func TestRemove(t *testing.T) {
	file, _ := Apply("mine.txt\n", Render([]string{"go"}, "*.exe"), Merge)
	rest, found, err := Remove(file)
	if err != nil || !found {
		t.Fatal(err)
	}
	if rest != "mine.txt\n" {
		t.Fatalf("rest = %q", rest)
	}
}

func TestBodyLineOffset(t *testing.T) {
	file, _ := Apply("", Render([]string{"go"}, "a\nb"), Merge)
	file = "one\ntwo\n" + file
	d, _ := Parse(file)
	lines := strings.Split(file, "\n")
	if got := lines[d.BodyLineOffset()]; got != "a" {
		t.Fatalf("line at offset = %q, want a", got)
	}
}
