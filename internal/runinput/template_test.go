package runinput

import (
	"errors"
	"testing"
)

func TestResolveTemplate(t *testing.T) {
	tests := []struct {
		name     string
		template string
		headless bool
		hasIssue bool
		hasSpec  bool
		want     string
	}{
		{name: "default is gated pr-factory", hasSpec: true, want: "pr-factory"},
		{name: "bare issue is pr-factory-headless", hasIssue: true, want: "pr-factory-headless"},
		{name: "bare headless is pr-factory-headless", headless: true, hasSpec: true, want: "pr-factory-headless"},
		{name: "headless swaps pr-factory", template: "pr-factory", headless: true, hasSpec: true, want: "pr-factory-headless"},
		{name: "headless swaps sdlc", template: "sdlc", headless: true, hasSpec: true, want: "sdlc-headless"},
		{name: "headless no-op on sdlc-headless", template: "sdlc-headless", headless: true, hasSpec: true, want: "sdlc-headless"},
		{name: "headless no-op on headless overlay path", template: "pipelines/sdlc-dry-headless.yaml", headless: true, want: "pipelines/sdlc-dry-headless.yaml"},
		{name: "sdlc with issue", template: "sdlc", hasIssue: true, want: "sdlc"},
		{name: "overlay needs no spec", template: "sdlc-dry", want: "sdlc-dry"},
		{name: "pipeline path needs no spec", template: "pipelines/sdlc-dry.yaml", want: "pipelines/sdlc-dry.yaml"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ResolveTemplate(tc.template, tc.headless, tc.hasIssue, tc.hasSpec)
			if err != nil {
				t.Fatalf("ResolveTemplate(%q) error: %v", tc.template, err)
			}
			if got != tc.want {
				t.Fatalf("ResolveTemplate(%q) = %q, want %q", tc.template, got, tc.want)
			}
		})
	}
}

func TestResolveTemplateInputRequired(t *testing.T) {
	tests := []struct {
		template string
		headless bool
		product  string
	}{
		{template: "pr-factory", product: "pr-factory"},
		{template: "pr-factory-headless", product: "pr-factory"},
		{template: "", headless: true, product: "pr-factory"},
		{template: "sdlc", product: "sdlc"},
		{template: "sdlc-headless", product: "sdlc"},
	}
	for _, tc := range tests {
		_, err := ResolveTemplate(tc.template, tc.headless, false, false)
		var required *InputRequiredError
		if !errors.As(err, &required) {
			t.Fatalf("ResolveTemplate(%q) error = %v, want *InputRequiredError", tc.template, err)
		}
		if required.Product != tc.product {
			t.Fatalf("ResolveTemplate(%q) product = %q, want %q", tc.template, required.Product, tc.product)
		}
	}
}

func TestResolveTemplateSpecAndIssue(t *testing.T) {
	_, err := ResolveTemplate("sdlc", false, true, true)
	if !errors.Is(err, ErrSpecAndIssue) {
		t.Fatalf("error = %v, want ErrSpecAndIssue", err)
	}
}

func TestResolveTemplateHeadlessUnsupported(t *testing.T) {
	for _, id := range []string{"demo", "sdlc-dry", "pipelines/sdlc-dry.yaml"} {
		_, err := ResolveTemplate(id, true, false, true)
		var unsupported *HeadlessUnsupportedError
		if !errors.As(err, &unsupported) {
			t.Fatalf("ResolveTemplate(%q, headless) error = %v, want *HeadlessUnsupportedError", id, err)
		}
		if unsupported.TemplateID != id {
			t.Fatalf("TemplateID = %q, want %q", unsupported.TemplateID, id)
		}
	}
}
