import type { Workspace, WorkspaceFileChange, WorkspaceFileContent, WorkspaceFileDraft, WorkspaceFileEntry } from './workspace';

export type ProjectWorkspaceFile = WorkspaceFileEntry;
export type ProjectWorkspace = Workspace;
export type ProjectWorkspaceFileContent = WorkspaceFileContent;
export type ProjectWorkspaceFileChange = WorkspaceFileChange;
export type ProjectWorkspaceFileDraft = WorkspaceFileDraft;

export interface ProjectWorkspaceUpdateManifest {
	fileTreeRevision: string;
	fileChanges: ProjectWorkspaceFileChange[];
}

export interface CreateProjectWorkspaceManifest {
	fileChanges: ProjectWorkspaceFileChange[];
}
