export type UploadKind = 'image' | 'volume-backup' | 'build-workspace' | 'project-archive';

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
	origin?: 'project-export' | 'project-bundle';
	originId?: string;
}
