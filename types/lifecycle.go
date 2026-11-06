package types

type (
	LifecycleExtraMount struct {
		Source   string
		Target   string
		Readonly bool
	}
)

const (
	LifecycleDefaultTimeoutSec = 60

	LifecycleDefaultMaxTimeoutSec = 300
)
