package search

import "testing"

var names = []string{"go", "godot", "google", "python", "visualstudiocode", "c++"}

func TestFuzzyPrefersExact(t *testing.T) {
	hits, err := Filter(names, "go", Fuzzy)
	if err != nil || names[hits[0].Index] != "go" {
		t.Fatalf("first = %v, err %v", hits, err)
	}
	hits, _ = Filter(names, "vsc", Fuzzy)
	if len(hits) != 1 || names[hits[0].Index] != "visualstudiocode" {
		t.Fatalf("vsc hits = %v", hits)
	}
}

func TestExactAndRegex(t *testing.T) {
	hits, _ := Filter(names, "OO", Exact)
	if len(hits) != 1 || names[hits[0].Index] != "google" {
		t.Fatalf("exact = %v", hits)
	}
	if got := hits[0].Matched; len(got) != 2 || got[0] != 1 {
		t.Fatalf("matched = %v", got)
	}
	hits, _ = Filter(names, `^go(dot)?$`, Regex)
	if len(hits) != 2 {
		t.Fatalf("regex = %v", hits)
	}
	if _, err := Filter(names, "(", Regex); err == nil {
		t.Fatal("expected an invalid regex error")
	}
	hits, _ = Filter(names, "c++", Exact)
	if len(hits) != 1 {
		t.Fatalf("c++ = %v", hits)
	}
}

func TestEmptyQueryKeepsOrder(t *testing.T) {
	hits, _ := Filter(names, "  ", Regex)
	if len(hits) != len(names) || hits[2].Index != 2 {
		t.Fatalf("hits = %v", hits)
	}
}

func TestParseMode(t *testing.T) {
	for in, want := range map[string]Mode{"": Fuzzy, "EXACT": Exact, "regexp": Regex} {
		if got, err := ParseMode(in); err != nil || got != want {
			t.Errorf("ParseMode(%q) = %v, %v", in, got, err)
		}
	}
	if _, err := ParseMode("glob"); err == nil {
		t.Fatal("expected error")
	}
}
