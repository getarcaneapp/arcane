export interface ProjectFile {
	path: string;
	relativePath: string;
	name: string;
	isDirectory: boolean;
	size: number;
	modTime?: string;
	protected?: boolean;
	content?: string;
}

export interface ProjectFileDraft {
	relativePath: string;
	isDirectory?: boolean;
	content?: string;
}

export type ProjectFileChangeOperation = 'create_file' | 'create_folder' | 'update_file' | 'rename' | 'move' | 'delete';

export interface ProjectFileChange {
	operation: ProjectFileChangeOperation;
	relativePath: string;
	newName?: string;
	newParentPath?: string;
	content?: string;
	recursive?: boolean;
}
