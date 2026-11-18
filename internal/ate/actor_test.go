package ate_test

import (
	"testing"

	"github.com/savitharaghunathan/shuttle/internal/ate"
)

func TestActorName(t *testing.T) {
	got, err := ate.ActorName("Acme", "Widget_App", "claude")
	if err != nil {
		t.Fatal(err)
	}
	if got != "acme-widget-app-claude" {
		t.Fatalf("got %q", got)
	}
}

func TestActorNameRejectsEmpty(t *testing.T) {
	if _, err := ate.ActorName("", "repo", "claude"); err == nil {
		t.Fatal("expected error")
	}
}

func TestTargetHeader(t *testing.T) {
	if got := ate.TargetHeader("shuttle", "acme-repo-claude"); got != "shuttle/acme-repo-claude" {
		t.Fatalf("got %q", got)
	}
}

func TestConfigValidate(t *testing.T) {
	err := (ate.Config{}).Validate()
	if err == nil {
		t.Fatal("expected error")
	}
	cfg := ate.Config{
		Endpoint: "localhost:8443", Atespace: "shuttle",
		ActorTemplate: "shuttle-claude", TaskURL: "http://127.0.0.1:8000",
	}
	if err := cfg.Validate(); err != nil {
		t.Fatal(err)
	}
}
