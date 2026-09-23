package compose

import (
	"strings"
	"testing"

	"github.com/kwmx/gignore/internal/catalog"
)

func tmpl(name, content string) *catalog.Template {
	return &catalog.Template{Key: catalog.Normalize(name), Name: name, Content: content}
}

func TestComposeHeadersAndDedupe(t *testing.T) {
	a := tmpl("A", "\n*.log\nbuild/\n\n\n# comment\n")
	b := tmpl("B", "*.log\ndist/\n# comment\n")
	res := Compose([]*catalog.Template{a, b}, Options{Dedupe: true})
	want := "### A ###\n*.log\nbuild/\n\n# comment\n\n### B ###\ndist/\n# comment\n"
	if res.Text != want {
		t.Fatalf("got:\n%q\nwant:\n%q", res.Text, want)
	}
	if res.Dropped != 1 {
		t.Fatalf("dropped = %d, want 1", res.Dropped)
	}
}

func TestDedupeKeepsRuleAfterNegation(t *testing.T) {
	// B re-includes keep.log; C's *.log must survive to re-ignore it.
	a := tmpl("A", "*.log")
	b := tmpl("B", "!keep.log")
	c := tmpl("C", "*.log")
	res := Compose([]*catalog.Template{a, b, c}, Options{Dedupe: true})
	if strings.Count(res.Text, "*.log") != 2 || res.Dropped != 0 {
		t.Fatalf("negation-aware dedupe failed:\n%s", res.Text)
	}
}

func TestSectionAt(t *testing.T) {
	text := Compose([]*catalog.Template{tmpl("Go", "*.exe"), tmpl("Node", "node_modules/")}, Options{}).Text
	lines := strings.Split(text, "\n")
	for i, l := range lines {
		if l == "node_modules/" {
			if got := SectionAt(text, i); got != "Node" {
				t.Fatalf("SectionAt = %q, want Node", got)
			}
			return
		}
	}
	t.Fatal("rule not found")
}

func TestCRLFNormalized(t *testing.T) {
	res := Compose([]*catalog.Template{tmpl("W", "a\r\nb\r\n")}, Options{})
	if strings.Contains(res.Text, "\r") {
		t.Fatalf("carriage return left in output: %q", res.Text)
	}
}
