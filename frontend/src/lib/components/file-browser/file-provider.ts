import type { FileEntry } from '#lib/types/shared';

export interface FileProvider {
	list: (path: string) => Promise<FileEntry[]>;
	mkdir: (path: string) => Promise<unknown>;
	upload: (path: string, file: File) => Promise<unknown>;
	delete: (path: string) => Promise<unknown>;
	download: (path: string) => Promise<void>;
	getContent: (path: string) => Promise<{ content: string }>;
}

export function sortFileEntries(files: FileEntry[]): FileEntry[] {
	return files.sort((a, b) => {
		if (a.isDirectory && !b.isDirectory) return -1;
		if (!a.isDirectory && b.isDirectory) return 1;
		return a.name.localeCompare(b.name);
	});
}
