package backup

// RepositoryObservation describes a read-only S3 metadata check.
type RepositoryObservation struct {
	Available         bool
	Reason            string
	SnapshotAvailable bool
}
