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

func TestParseSchemaVersionRules(t *testing.T) {
	t.Setenv("TEST_SCHEMA_RULES", "order.created:1-3,payment.captured:2-2,bad")
	got := parseSchemaVersionRules("TEST_SCHEMA_RULES")
	if len(got) != 2 {
		t.Fatalf("expected 2 rules, got %#v", got)
	}
	if got["order.created"].Min != 1 || got["order.created"].Max != 3 {
		t.Fatalf("bad range: %#v", got["order.created"])
	}
}

func TestParseRequiredFieldRules(t *testing.T) {
	t.Setenv("TEST_REQ_RULES", "order.created=orderId|customerId;payment.captured=paymentId")
	got := parseRequiredFieldRules("TEST_REQ_RULES")
	if len(got) != 2 {
		t.Fatalf("expected 2 rules, got %#v", got)
	}
	if len(got["order.created"]) != 2 {
		t.Fatalf("unexpected fields: %#v", got["order.created"])
	}
}

func TestParseInt64Map(t *testing.T) {
	t.Setenv("TEST_I64_MAP", "tokA=100,tokB=200,bad")
	got := parseInt64Map("TEST_I64_MAP")
	if len(got) != 2 {
		t.Fatalf("expected 2 entries, got %#v", got)
	}
	if got["tokA"] != 100 || got["tokB"] != 200 {
		t.Fatalf("unexpected values: %#v", got)
	}
}

func TestConfigValidateSuccess(t *testing.T) {
	cfg := FromEnv()
	cfg.RejectUnknownEventTypes = true
	cfg.EventTypeRegistry = []string{"order.created", "order.cancelled"}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("expected valid config, got %v", err)
	}
}

func TestConfigValidateRejectUnknownTypesRequiresRegistry(t *testing.T) {
	cfg := FromEnv()
	cfg.RejectUnknownEventTypes = true
	cfg.EventTypeRegistry = nil
	if err := cfg.Validate(); err == nil {
		t.Fatalf("expected validation error for empty event type registry")
	}
}

func TestConfigValidateRejectsBackoffRange(t *testing.T) {
	cfg := FromEnv()
	cfg.OutboxBackoffBaseMs = 5000
	cfg.OutboxBackoffMaxMs = 1000
	if err := cfg.Validate(); err == nil {
		t.Fatalf("expected validation error for invalid backoff range")
	}
}
