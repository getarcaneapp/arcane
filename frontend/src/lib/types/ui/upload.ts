export interface ChunkedUploadProgress {
	chunksDone: number;
	totalChunks: number;
	bytesDone: number;
	totalBytes: number;
}

export type UploadProgressCallback = (progress: ChunkedUploadProgress) => void;
