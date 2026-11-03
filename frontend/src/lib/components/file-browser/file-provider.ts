import type { UploadProgressCallback } from '#lib/types/ui/upload.js';
import type { WorkspaceFileEntry } from '#lib/types/workspace.js';

export interface FileProvider {
	list: (path: string) => Promise<WorkspaceFileEntry[]>;
	mkdir: (path: string) => Promise<unknown>;
	upload: (path: string, file: File, onProgress?: UploadProgressCallback) => Promise<unknown>;
	delete: (path: string) => Promise<unknown>;
	download: (path: string) => Promise<void>;
	getContent: (path: string) => Promise<{ content: string }>;
}

export function sortFileEntries(files: WorkspaceFileEntry[]): WorkspaceFileEntry[] {
	return files.sort((a, b) => {
		if (a.isDirectory && !b.isDirectory) return -1;
		if (!a.isDirectory && b.isDirectory) return 1;
		return a.name.localeCompare(b.name);
	});
}
