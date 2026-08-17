export type WorkspaceReadOnlyReason = 'binary' | 'too_large' | 'symlink' | 'special' | 'gitops_managed';

export interface WorkspaceFileEntry {
	path: string;
	relativePath: string;
	name: string;
	isDirectory: boolean;
	size: number;
	modTime?: string;
	mode?: string;
	isSymlink: boolean;
	linkTarget?: string;
	editable: boolean;
	readOnlyReason?: WorkspaceReadOnlyReason;
}

export interface Workspace {
	files: WorkspaceFileEntry[];
	fileTreeRevision: string;
	fileTreeTruncated: boolean;
	activityId?: string;
}

export interface WorkspaceFileContent {
	path: string;
	relativePath: string;
	name: string;
	size: number;
	mimeType: string;
	content?: string;
	editable: boolean;
	readOnlyReason?: WorkspaceReadOnlyReason;
}

export type WorkspaceFileChangeOperation = 'create_file' | 'create_folder' | 'update_file' | 'rename' | 'move' | 'delete';

export interface WorkspaceFileChange {
	operation: WorkspaceFileChangeOperation;
	relativePath: string;
	newName?: string;
	newParentPath?: string;
	uploadIndex?: number;
	baselineIndex?: number;
	recursive?: boolean;
}

export interface WorkspaceFileDraft {
	relativePath: string;
	isDirectory?: boolean;
	content?: string;
}
