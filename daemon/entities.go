package daemon

const (
	// GitTagStateMode is a mode of state management where Flux uses a git tag for managing Flux state
	GitTagStateMode = "GitTag"

	// ConfigMapStateMode is a mode of state management where Flux uses a kubernetes ConfigMap for managing Flux state
	ConfigMapStateMode = "ConfigMap"
)
