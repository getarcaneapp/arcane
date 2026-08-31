import type { Workspace, WorkspaceFileChange, WorkspaceFileContent, WorkspaceFileDraft } from './workspace';

export type ProjectWorkspace = Workspace;
export type ProjectWorkspaceFileContent = WorkspaceFileContent;
export type ProjectWorkspaceFileChange = WorkspaceFileChange;
export type ProjectWorkspaceFileDraft = WorkspaceFileDraft;

export interface ProjectWorkspaceUpdateManifest {
	fileTreeRevision: string;
	fileChanges: ProjectWorkspaceFileChange[];
}
