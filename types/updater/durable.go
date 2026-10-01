package updater

// SingleUpdateCommand contains the persisted inputs for an asynchronous update.
type SingleUpdateCommand struct {
	ContainerID string
	ActivityID  string
	UserID      string
	KeyID       string
}

type SingleUpdateState struct {
	Command SingleUpdateCommand
	Status  string
	Target  *FrozenUpdateTarget
	Result  *Result
	Failure string
}

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
