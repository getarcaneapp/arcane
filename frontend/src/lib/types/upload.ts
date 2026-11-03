export type UploadKind = 'image' | 'volume-backup' | 'build-workspace';

export interface UploadSession {
	createdAt: string;
	id: string;
	kind: UploadKind;
	filename: string;
	receivedChunks: number[];
	size: number;
	chunkSize: number;
	totalChunks: number;
	complete: boolean;
}
