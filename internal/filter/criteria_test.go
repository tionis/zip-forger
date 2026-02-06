package filter

import "testing"

func TestCriteriaMatchWithGlobExcludeAndExtension(t *testing.T) {
	compiled, err := Compile(Criteria{
		IncludeGlobs: []string{"rules/**/*.pdf"},
		ExcludeGlobs: []string{"**/*draft*"},
		Extensions:   []string{"pdf"},
	})
	if err != nil {
		t.Fatalf("Compile failed: %v", err)
	}

	if !compiled.Match("rules/core/v1/guide.pdf") {
		t.Fatalf("expected match")
	}
	if compiled.Match("rules/core/v1/guide-draft.pdf") {
		t.Fatalf("expected exclude to block draft file")
	}
	if compiled.Match("rules/core/v1/guide.txt") {
		t.Fatalf("expected extension to block non-pdf")
	}
}

func TestCriteriaPrefixOnly(t *testing.T) {
	compiled, err := Compile(Criteria{
		PathPrefixes: []string{"session-notes/2025"},
	})
	if err != nil {
		t.Fatalf("Compile failed: %v", err)
	}

	if !compiled.Match("session-notes/2025/intro.md") {
		t.Fatalf("expected prefix match")
	}
	if compiled.Match("session-notes/2024/intro.md") {
		t.Fatalf("expected non-prefix path to fail")
	}
}
