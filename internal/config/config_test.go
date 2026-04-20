package config

import "testing"

func TestSplitOr(t *testing.T) {
	t.Setenv("TEST_SPLIT", " a, b ,,c ")
	got := splitOr("TEST_SPLIT", "")
	if len(got) != 3 || got[0] != "a" || got[1] != "b" || got[2] != "c" {
		t.Fatalf("unexpected split result: %#v", got)
	}
}

func TestBoolOr(t *testing.T) {
	t.Setenv("TEST_BOOL", "true")
	if !boolOr("TEST_BOOL", false) {
		t.Fatalf("expected boolOr true")
	}
	t.Setenv("TEST_BOOL", "0")
	if boolOr("TEST_BOOL", true) {
		t.Fatalf("expected boolOr false")
	}
}

func TestIntOr(t *testing.T) {
	t.Setenv("TEST_INT", "42")
	if got := intOr("TEST_INT", 1); got != 42 {
		t.Fatalf("expected 42, got %d", got)
	}
	t.Setenv("TEST_INT", "bad")
	if got := intOr("TEST_INT", 7); got != 7 {
		t.Fatalf("expected fallback 7, got %d", got)
	}
}
