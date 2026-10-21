package types

// LifecycleExtraMount is one configured bind mount for a lifecycle runner container.
type LifecycleExtraMount struct {
	Source   string
	Target   string
	Readonly bool
}

// LifecycleDefaultTimeoutSec is the lifecycle timeout when a sync has no override.
const LifecycleDefaultTimeoutSec = 60

// LifecycleDefaultMaxTimeoutSec is the default lifecycle timeout ceiling.
const LifecycleDefaultMaxTimeoutSec = 300
