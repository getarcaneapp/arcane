export interface VolumeWorkspaceFile {
	path: string;
	relativePath: string;
	name: string;
	isDirectory: boolean;
	size: number;
	modTime?: string;
	mode?: string;
	isSymlink?: boolean;
	linkTarget?: string;
}

export interface VolumeWorkspace {
	files: VolumeWorkspaceFile[];
	fileTreeRevision: string;
	fileTreeTruncated: boolean;
	activityId?: string;
}

export interface VolumeWorkspaceFileContent {
	path: string;
	relativePath: string;
	name: string;
	size: number;
	mimeType: string;
	content?: string;
	editable: boolean;
	readOnlyReason?: 'binary' | 'too_large' | 'symlink' | 'special' | 'restore_pending';
}

export type VolumeFileChangeOperation =
	| 'create_file'
	| 'create_folder'
	| 'update_file'
	| 'rename'
	| 'move'
	| 'delete'
	| 'restore_file';

export interface VolumeFileChange {
	operation: VolumeFileChangeOperation;
	relativePath: string;
	newName?: string;
	newParentPath?: string;
	content?: string;
	uploadIndex?: number;
	backupId?: string;
	recursive?: boolean;
}

export interface VolumeWorkspaceUpdateManifest {
	fileTreeRevision: string;
	fileChanges: VolumeFileChange[];
}
