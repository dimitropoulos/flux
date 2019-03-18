package nativestate

import (
	"context"
	fluxsync "github.com/weaveworks/flux/sync"
)

// READONLY-NOTE: Once the general direction of this work is agreed upon, this package will use a real Kuberentes Resource instead of an in-memory Shim.

// NativeShim is a fake/placeholder in-memory structure that represents the data in a native Kubernetes Resource of some kind.
type NativeShim struct {
	data struct {
		FluxSync struct {
			Revision string
			Message  string
		}
	}
}

var nativeShim NativeShim

// NativeSyncProvider keeps information related to the native state of a sync marker stored in a "native" kubernetes resource.
type NativeSyncProvider struct {
	revision string
}

func NewNativeSyncProvider() NativeSyncProvider {
	return NativeSyncProvider{}
}

// GetRevision gets the revision of the current sync marker (representing the place flux has synced to)
func (p NativeSyncProvider) GetRevision(ctx context.Context) (string, error) {
	return nativeShim.data.FluxSync.Revision, nil
}

// UpdateMarker updates the revision the sync marker points to
func (p NativeSyncProvider) UpdateMarker(ctx context.Context, syncMarkerAction fluxsync.SyncMarkerAction) error {
	nativeShim.data.FluxSync.Revision = syncMarkerAction.Revision
	nativeShim.data.FluxSync.Message = syncMarkerAction.Message
	return nil
}

// DeleteMarker resets the state of the object
func (p NativeSyncProvider) DeleteMarker(ctx context.Context) error {
	nativeShim = NativeShim{}
	return nil
}
