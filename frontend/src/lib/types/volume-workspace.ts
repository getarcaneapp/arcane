import type {
	Workspace,
	WorkspaceFileChange,
	WorkspaceFileContent,
	WorkspaceFileEntry,
	WorkspaceReadOnlyReason
} from './workspace';

export type VolumeWorkspaceFile = WorkspaceFileEntry;
export type VolumeWorkspace = Workspace;
export interface VolumeWorkspaceFileContent extends Omit<WorkspaceFileContent, 'readOnlyReason'> {
	readOnlyReason?: WorkspaceReadOnlyReason | 'restore_pending';
}

export type VolumeWorkspaceFileChangeOperation =
	| 'create_file'
	| 'create_folder'
	| 'update_file'
	| 'rename'
	| 'move'
	| 'delete'
	| 'restore_file';

export interface VolumeWorkspaceFileChange extends Omit<WorkspaceFileChange, 'operation'> {
	operation: VolumeWorkspaceFileChangeOperation;
	backupId?: string;
}

export interface VolumeWorkspaceUpdateManifest {
	fileTreeRevision: string;
	fileChanges: VolumeWorkspaceFileChange[];
}
