// Package lifecycle contains lifecycle-hook values shared across Arcane modules.
package lifecycle

// ExtraMount is one configured bind mount for a lifecycle runner container.
type ExtraMount struct {
	Source   string
	Target   string
	Readonly bool
}
