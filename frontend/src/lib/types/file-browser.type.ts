export interface FileEntry {
	name: string;
	path: string;
	isDirectory: boolean;
	size: number;
	modTime: string;
	mode: string;
	isSymlink: boolean;
	linkTarget?: string;
}

export interface FileContentResponse {
	content: string; // Base64 encoded bytes from Go
	mimeType: string;
}

export interface BackupEntry {
	id: string;
	volumeName: string;
	size: number;
	createdAt: string;
}
