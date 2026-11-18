package ate

import (
	"context"
	"fmt"
	"sync"

	"github.com/savitharaghunathan/shuttle/internal/ate/ateapi"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// FakeControl is an in-memory Control for tests.
type FakeControl struct {
	mu     sync.Mutex
	Actors map[string]*ateapi.Actor // key: atespace/name

	ResumeErr  error
	SuspendErr error
	CreateErr  error
	GetErr     error

	Resumes  int
	Suspends int
}

func NewFakeControl() *FakeControl {
	return &FakeControl{Actors: map[string]*ateapi.Actor{}}
}

func key(atespace, name string) string { return atespace + "/" + name }

func (f *FakeControl) GetActor(_ context.Context, atespace, name string) (*ateapi.Actor, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.GetErr != nil {
		return nil, f.GetErr
	}
	a, ok := f.Actors[key(atespace, name)]
	if !ok {
		return nil, status.Error(codes.NotFound, "actor not found")
	}
	return a, nil
}

func (f *FakeControl) CreateActor(_ context.Context, atespace, name, templateName string) (*ateapi.Actor, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.CreateErr != nil {
		return nil, f.CreateErr
	}
	k := key(atespace, name)
	if _, ok := f.Actors[k]; ok {
		return nil, fmt.Errorf("actor already exists")
	}
	a := &ateapi.Actor{
		Metadata:      &ateapi.ResourceMetadata{Atespace: atespace, Name: name},
		ActorTemplate: &ateapi.ObjectRef{Atespace: atespace, Name: templateName},
		Status:        &ateapi.ActorStatus{State: ateapi.ActorState_ACTOR_STATE_SUSPENDED},
	}
	f.Actors[k] = a
	return a, nil
}

func (f *FakeControl) ResumeActor(_ context.Context, atespace, name string) (*ateapi.Actor, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.Resumes++
	if f.ResumeErr != nil {
		return nil, f.ResumeErr
	}
	a, ok := f.Actors[key(atespace, name)]
	if !ok {
		return nil, status.Error(codes.NotFound, "actor not found")
	}
	a.Status = &ateapi.ActorStatus{State: ateapi.ActorState_ACTOR_STATE_RUNNING}
	return a, nil
}

func (f *FakeControl) SuspendActor(_ context.Context, atespace, name string) (*ateapi.Actor, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.Suspends++
	if f.SuspendErr != nil {
		return nil, f.SuspendErr
	}
	a, ok := f.Actors[key(atespace, name)]
	if !ok {
		return nil, status.Error(codes.NotFound, "actor not found")
	}
	a.Status = &ateapi.ActorStatus{State: ateapi.ActorState_ACTOR_STATE_SUSPENDED}
	return a, nil
}

func (f *FakeControl) Close() error { return nil }
