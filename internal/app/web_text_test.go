package app

import "testing"

func TestNormalizeFieldValuesForKeyMultiline(t *testing.T) {
	values := []string{"line1", "line2"}
	normalized := normalizeFieldValuesForKey("description", values)
	if len(normalized) != 1 {
		t.Fatalf("expected single merged value, got %d", len(normalized))
	}
	if normalized[0] != "line1\\nline2" {
		t.Fatalf("expected escaped newlines, got %q", normalized[0])
	}
}

func TestNormalizeFieldValuesForKeySingleLine(t *testing.T) {
	values := []string{"  foo  ", "bar  "}
	normalized := normalizeFieldValuesForKey("game", values)
	if len(normalized) != 2 {
		t.Fatalf("expected 2 values, got %d", len(normalized))
	}
	if normalized[0] != "foo" || normalized[1] != "bar" {
		t.Fatalf("unexpected normalized values: %v", normalized)
	}
}

func TestNormalizeFieldValueForDisplay(t *testing.T) {
	out := normalizeFieldValueForDisplay("summary", "line1\\nline2")
	if out != "line1\nline2" {
		t.Fatalf("expected newline conversion, got %q", out)
	}
	plain := normalizeFieldValueForDisplay("game", "value")
	if plain != "value" {
		t.Fatalf("unexpected plain value %q", plain)
	}
}
