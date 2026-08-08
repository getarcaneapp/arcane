package models

// SystemUser is the actor recorded for work Arcane performs on its own behalf —
// scheduled jobs, startup reconciliation, GitOps syncs — rather than in response
// to a signed-in user.
var SystemUser = User{
	Username: "System",
}
