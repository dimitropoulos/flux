package entities

const (
	// GitTagStateMode is a mode of state management where Flux uses a git tag for managing Flux state
	GitTagStateMode = "GitTag"

	// NativeStateMode is a mode of state management where Flux uses native Kubernetes resources for managing Flux state
	NativeStateMode = "Native"
)
