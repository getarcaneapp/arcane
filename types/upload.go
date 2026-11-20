package types

import (
	"time"
)

type (
	CreateUploadSessionRequest struct {
		Filename  string `json:"filename" required:"true" doc:"Original filename; validated per kind"`
		Size      int64  `json:"size" required:"true" minimum:"1" doc:"Total file size in bytes"`
		ChunkSize int64  `json:"chunkSize,omitempty" doc:"Chunk size in bytes; server default and bounds apply"`
	}

	UploadSession struct {
		CreatedAt time.Time `json:"createdAt"`
		ID        string    `json:"id"`
		Kind      string    `json:"kind"`
		Filename  string    `json:"filename"`
		// Origin and OriginID name what wrote the session on the server, such as an export of a project; both are
		// empty for an upload.
		Origin         string `json:"origin,omitempty" doc:"What wrote the session on the server: project-export or project-bundle; empty for an upload"`
		OriginID       string `json:"originId,omitempty" doc:"ID of the resource the session was written from"`
		ReceivedChunks []int  `json:"receivedChunks"`
		Size           int64  `json:"size"`
		ChunkSize      int64  `json:"chunkSize"`
		TotalChunks    int    `json:"totalChunks"`
		Complete       bool   `json:"complete"`
	}

	UploadConsumeRequest struct {
		UploadID string `json:"uploadId" required:"true" doc:"ID of a complete upload session"`
	}
)

const (
	UploadKindImage          = "image"
	UploadKindVolumeBackup   = "volume-backup"
	UploadKindBuildWorkspace = "build-workspace"
	UploadKindProjectArchive = "project-archive"

	// UploadOriginProjectExport is a project's files and UploadOriginProjectBundle its files with volume data, written
	// by an export of the project.
	UploadOriginProjectExport = "project-export"
	UploadOriginProjectBundle = "project-bundle"
	// Chunk size bounds keep every chunk request safely under common reverse-proxy
	// and WAF body limits (~100 MB).

	UploadDefaultChunkSize int64 = 10 * 1024 * 1024
	UploadMinChunkSize     int64 = 1 * 1024 * 1024
	UploadMaxChunkSize     int64 = 50 * 1024 * 1024
)
