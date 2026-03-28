package nameussy

import (
	"testing"
)

// ---------------------------------------------------------------------------
// GenerateName (horse names)
// ---------------------------------------------------------------------------

func TestGenerateName_NonEmpty(t *testing.T) {
	for i := 0; i < 50; i++ {
		name := GenerateName()
		if name == "" {
			t.Fatalf("GenerateName returned empty string on iteration %d", i)
		}
	}
}

func TestGenerateName_Variety(t *testing.T) {
	// Generate many names and verify we get at least a few unique ones.
	// With 7 patterns and many word combos, collisions should be rare.
	seen := make(map[string]bool)
	iterations := 100

	for i := 0; i < iterations; i++ {
		seen[GenerateName()] = true
	}

	// We should get significant variety — at least 10 unique names out of 100
	if len(seen) < 10 {
		t.Errorf("expected at least 10 unique names from %d generations, got %d", iterations, len(seen))
	}
}

func TestGenerateName_ReasonableLength(t *testing.T) {
	for i := 0; i < 50; i++ {
		name := GenerateName()
		if len(name) < 3 {
			t.Errorf("name %q is too short (len=%d)", name, len(name))
		}
		if len(name) > 200 {
			t.Errorf("name %q is suspiciously long (len=%d)", name, len(name))
		}
	}
}

// ---------------------------------------------------------------------------
// GenerateStableName
// ---------------------------------------------------------------------------

func TestGenerateStableName_NonEmpty(t *testing.T) {
	for i := 0; i < 50; i++ {
		name := GenerateStableName()
		if name == "" {
			t.Fatalf("GenerateStableName returned empty string on iteration %d", i)
		}
	}
}

func TestGenerateStableName_Variety(t *testing.T) {
	seen := make(map[string]bool)
	iterations := 100

	for i := 0; i < iterations; i++ {
		seen[GenerateStableName()] = true
	}

	if len(seen) < 10 {
		t.Errorf("expected at least 10 unique stable names from %d generations, got %d", iterations, len(seen))
	}
}

func TestGenerateStableName_ReasonableLength(t *testing.T) {
	for i := 0; i < 50; i++ {
		name := GenerateStableName()
		if len(name) < 3 {
			t.Errorf("stable name %q is too short (len=%d)", name, len(name))
		}
		if len(name) > 200 {
			t.Errorf("stable name %q is suspiciously long (len=%d)", name, len(name))
		}
	}
}

// ---------------------------------------------------------------------------
// Individual pattern coverage — verify each pattern works
// ---------------------------------------------------------------------------

func TestPatternAdjectiveNoun(t *testing.T) {
	for i := 0; i < 10; i++ {
		name := patternAdjectiveNoun()
		if name == "" {
			t.Fatal("patternAdjectiveNoun returned empty string")
		}
	}
}

func TestPatternPossessive(t *testing.T) {
	for i := 0; i < 10; i++ {
		name := patternPossessive()
		if name == "" {
			t.Fatal("patternPossessive returned empty string")
		}
		// Should contain an apostrophe
		found := false
		for _, c := range name {
			if c == '\'' {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("expected possessive pattern to contain apostrophe, got %q", name)
		}
	}
}

func TestPatternGerund(t *testing.T) {
	for i := 0; i < 10; i++ {
		name := patternGerund()
		if name == "" {
			t.Fatal("patternGerund returned empty string")
		}
	}
}

func TestPatternSir(t *testing.T) {
	for i := 0; i < 10; i++ {
		name := patternSir()
		if name == "" {
			t.Fatal("patternSir returned empty string")
		}
		if name[:4] != "Sir " {
			t.Errorf("expected patternSir to start with 'Sir ', got %q", name)
		}
	}
}

func TestPatternDoubleAdjective(t *testing.T) {
	for i := 0; i < 10; i++ {
		name := patternDoubleAdjective()
		if name == "" {
			t.Fatal("patternDoubleAdjective returned empty string")
		}
	}
}

func TestPatternGit(t *testing.T) {
	for i := 0; i < 10; i++ {
		name := patternGit()
		if name == "" {
			t.Fatal("patternGit returned empty string")
		}
		if name[:4] != "git " {
			t.Errorf("expected patternGit to start with 'git ', got %q", name)
		}
	}
}

func TestPatternFood(t *testing.T) {
	for i := 0; i < 10; i++ {
		name := patternFood()
		if name == "" {
			t.Fatal("patternFood returned empty string")
		}
	}
}

// ---------------------------------------------------------------------------
// Stable name patterns
// ---------------------------------------------------------------------------

func TestStableAdjectiveNoun(t *testing.T) {
	for i := 0; i < 10; i++ {
		name := stableAdjectiveNoun()
		if name == "" {
			t.Fatal("stableAdjectiveNoun returned empty string")
		}
	}
}

func TestStablePossessiveAdjective(t *testing.T) {
	for i := 0; i < 10; i++ {
		name := stablePossessiveAdjective()
		if name == "" {
			t.Fatal("stablePossessiveAdjective returned empty string")
		}
	}
}

func TestStableFoodThemed(t *testing.T) {
	for i := 0; i < 10; i++ {
		name := stableFoodThemed()
		if name == "" {
			t.Fatal("stableFoodThemed returned empty string")
		}
	}
}

func TestStableDoubleNoun(t *testing.T) {
	for i := 0; i < 10; i++ {
		name := stableDoubleNoun()
		if name == "" {
			t.Fatal("stableDoubleNoun returned empty string")
		}
		// Should contain " & "
		found := false
		for j := 0; j < len(name)-2; j++ {
			if name[j:j+3] == " & " {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("expected '&' in double noun pattern, got %q", name)
		}
	}
}

func TestStableQuantum(t *testing.T) {
	for i := 0; i < 10; i++ {
		name := stableQuantum()
		if name == "" {
			t.Fatal("stableQuantum returned empty string")
		}
		// Should end with " LLC"
		if len(name) < 4 || name[len(name)-4:] != " LLC" {
			t.Errorf("expected stableQuantum to end with ' LLC', got %q", name)
		}
	}
}

// ---------------------------------------------------------------------------
// pick helper
// ---------------------------------------------------------------------------

func TestPick(t *testing.T) {
	list := []string{"a", "b", "c"}
	for i := 0; i < 20; i++ {
		result := pick(list)
		if result != "a" && result != "b" && result != "c" {
			t.Errorf("pick returned unexpected value %q", result)
		}
	}
}

func TestPick_SingleElement(t *testing.T) {
	list := []string{"only"}
	result := pick(list)
	if result != "only" {
		t.Errorf("expected 'only', got %q", result)
	}
}
