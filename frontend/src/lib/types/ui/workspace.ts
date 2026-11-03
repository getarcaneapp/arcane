export interface WorkspaceFileDraft {
	relativePath: string;
	isDirectory?: boolean;
	content?: string;
	file?: File;
}
