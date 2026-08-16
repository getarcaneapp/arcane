// Package upload defines the chunked upload-session wire types shared by the
// backend, frontend contract, and CLI.
package upload

import "time"

// Session kinds tie an upload session to the domain endpoint allowed to
// consume it.
const (
	KindImage          = "image"
	KindVolumeBackup   = "volume-backup"
	KindBuildWorkspace = "build-workspace"
)

// Chunk size bounds keep every chunk request safely under common reverse-proxy
// and WAF body limits (~100 MB).
const (
	DefaultChunkSize int64 = 10 * 1024 * 1024
	MinChunkSize     int64 = 1 * 1024 * 1024
	MaxChunkSize     int64 = 50 * 1024 * 1024
)

// CreateSessionRequest starts a chunked upload session.
type CreateSessionRequest struct {
	Filename  string `json:"filename" required:"true" doc:"Original filename; validated per kind"`
	Size      int64  `json:"size" required:"true" minimum:"1" doc:"Total file size in bytes"`
	ChunkSize int64  `json:"chunkSize,omitempty" doc:"Chunk size in bytes; server default and bounds apply"`
}

// Session describes a chunked upload session, including which chunks have been
// received so clients can resume by re-sending only the missing ones.
type Session struct {
	CreatedAt      time.Time `json:"createdAt"`
	ID             string    `json:"id"`
	Kind           string    `json:"kind"`
	Filename       string    `json:"filename"`
	ReceivedChunks []int     `json:"receivedChunks"`
	Size           int64     `json:"size"`
	ChunkSize      int64     `json:"chunkSize"`
	TotalChunks    int       `json:"totalChunks"`
	Complete       bool      `json:"complete"`
}

// ConsumeRequest references a complete upload session from a domain endpoint.
type ConsumeRequest struct {
	UploadID string `json:"uploadId" required:"true" doc:"ID of a complete upload session"`
}
