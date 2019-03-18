package sync

import (
	"context"
)

const (
	// GitTagStateMode is a mode of state management where Flux uses a git tag for managing Flux state
	GitTagStateMode = "GitTag"

	// NativeStateMode is a mode of state management where Flux uses native Kubernetes resources for managing Flux state
	NativeStateMode = "Native"
)

// READONLY-NOTE: this is precicely the same as the git.Commit struct.  Not sure if that's intentional (it was that way before with git.TagAction) but it seems like it might make more sense to be more forthwright about what SyncMarkerAction really represents... a Commit.  On the other hand, the two concepts (a sync marker action and a commit) are different things despite having identical data so I can see it either way.  please weigh in.
type SyncMarkerAction struct {
	SigningKey string
	Revision   string
	Message    string
}

type SyncProvider interface {
	GetRevision(ctx context.Context) (string, error)
	UpdateMarker(ctx context.Context, syncMarkerAction SyncMarkerAction) error
	DeleteMarker(ctx context.Context) error
}
