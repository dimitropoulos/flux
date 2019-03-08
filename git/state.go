package git

import (
	"context"
)

// READONLY-NOTE: this may belong in its own package since, after all, it's not git realated.
// It is only for the historical reason of the state being previously exclusively managed with a git tag that this code is here in the first place.

// ConfigMapShim is a fake/placeholder in-memory structure that represents the data in a configmap.  If this experiment pans out, it will be a full-blown (real) ConfigMap that is used instead of this struct
type ConfigMapShim struct {
	data struct {
		FluxSync SyncMarkerAction
	}
}

var configMapShim ConfigMapShim

func updateConfigMapState(ctx context.Context, configMapName GitRef, syncMarkerAction SyncMarkerAction) error {
	configMapShim.data.FluxSync = syncMarkerAction
	return nil
}

func getConfigMapSyncMarkerRevision(ctx context.Context) (GitRef, error) {
	revision := configMapShim.data.FluxSync.Revision
	return revision, nil
}
