package types

type (
	// LifecycleExtraMount is one configured bind mount for a lifecycle runner container.
	LifecycleExtraMount struct {
		Source   string
		Target   string
		Readonly bool
	}
)

const (
	// LifecycleDefaultTimeoutSec is the lifecycle timeout when a sync has no override.
	LifecycleDefaultTimeoutSec = 60

	// LifecycleDefaultMaxTimeoutSec is the default lifecycle timeout ceiling.
	LifecycleDefaultMaxTimeoutSec = 300
)
