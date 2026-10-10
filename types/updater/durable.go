package updater

// FrozenUpdateTarget identifies the selected container and immutable image intent.
type FrozenUpdateTarget struct {
	ContainerID          string
	ContainerName        string
	ComposeProject       string
	ComposeService       string
	ComposeNumber        string
	BaselineImageID      string
	BaselineStartedAt    string
	BaselineRestartCount int
	DesiredImageRef      string
	DesiredDigest        string
}
