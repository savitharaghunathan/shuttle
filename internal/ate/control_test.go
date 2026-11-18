package ate_test

import (
	"context"
	"testing"

	"github.com/savitharaghunathan/shuttle/internal/ate"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestEnsureActorCreatesWhenMissing(t *testing.T) {
	fake := ate.NewFakeControl()
	actor, err := ate.EnsureActor(context.Background(), fake, "shuttle", "acme-repo-claude", "shuttle-claude")
	if err != nil {
		t.Fatal(err)
	}
	if actor.GetMetadata().GetName() != "acme-repo-claude" {
		t.Fatalf("name=%q", actor.GetMetadata().GetName())
	}
	// second call should get existing
	actor2, err := ate.EnsureActor(context.Background(), fake, "shuttle", "acme-repo-claude", "shuttle-claude")
	if err != nil {
		t.Fatal(err)
	}
	if actor2.GetMetadata().GetName() != actor.GetMetadata().GetName() {
		t.Fatal("expected same actor")
	}
	if len(fake.Actors) != 1 {
		t.Fatalf("actors=%d", len(fake.Actors))
	}
}

func TestIsNotFound(t *testing.T) {
	if !ate.IsNotFound(status.Error(codes.NotFound, "x")) {
		t.Fatal("expected not found")
	}
	if ate.IsNotFound(status.Error(codes.Internal, "x")) {
		t.Fatal("did not expect not found")
	}
}
