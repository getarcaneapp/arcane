package types

type (
	LogStreamOptions struct {
		Tail       string
		Since      string
		Follow     bool
		Timestamps bool
	}
)
