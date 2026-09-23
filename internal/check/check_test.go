package check

import (
	"strings"
	"testing"

	"github.com/kwmx/gignore/internal/block"
	"github.com/kwmx/gignore/internal/catalog"
)

func codes(fs []Finding) string {
	var out []string
	for _, f := range fs {
		out = append(out, f.Code)
	}
	return strings.Join(out, ",")
}

func TestRuleChecks(t *testing.T) {
	content := "build/\n!build/keep.txt\nlogs/*\n!logs/keep.txt\nfoo \nC:\\temp\n*.log\n*.log\n"
	fs := Run(Input{Content: content})
	if got := codes(fs); got != "unreachable-negation,trailing-space,backslash,duplicate" {
		t.Fatalf("codes = %s", got)
	}
	if fs[0].Line != 2 {
		t.Fatalf("negation finding on line %d", fs[0].Line)
	}
}

func TestBlockChecks(t *testing.T) {
	c := catalog.New(catalog.Options{}, []catalog.Template{{Key: "go", Name: "Go", Content: "*.exe"}})
	file, _ := block.Apply("", block.Render([]string{"go"}, "### Go ###\n*.exe"), block.Merge)
	file += "*.exe\n"
	fs := Run(Input{
		Content:     file,
		Catalog:     c,
		Regenerated: func([]string) (string, error) { return "### Go ###\n*.exe\n*.dll", nil },
		Detected:    []string{"go", "node"},
	})
	if got := codes(fs); got != "stale-block,redundant,missing-template" {
		t.Fatalf("codes = %s", got)
	}
	if Worst(fs) != Warning {
		t.Fatalf("worst = %v", Worst(fs))
	}

	fs = Run(Input{Content: block.Render([]string{"nope"}, "x"), Catalog: c})
	if codes(fs) != "unknown-template" || Worst(fs) != Error {
		t.Fatalf("codes = %s", codes(fs))
	}
}

func TestNegationsInsideBlockIgnored(t *testing.T) {
	file := block.Render([]string{"go"}, "bin/\n!bin/keep")
	if fs := Run(Input{Content: file}); len(fs) != 0 {
		t.Fatalf("template content should not be flagged: %v", codes(fs))
	}
}
