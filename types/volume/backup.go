package volume

type BackupEntry struct {
	ID         string `json:"id" doc:"Unique identifier of the backup"`
	VolumeName string `json:"volumeName" doc:"Name of the volume"`
	Size       int64  `json:"size" doc:"Size of the backup archive in bytes"`
	CreatedAt  string `json:"createdAt" doc:"When the backup was created"`
}
