import { toast } from 'svelte-sonner';
import { m } from '#lib/paraglide/messages.js';
import {
	joinWorkspaceFilePath,
	validateWorkspaceFileName,
	workspaceFileParentPath,
	type WorkspaceDisplayEntry
} from '#lib/utils/workspace-files.js';

export type ProjectWorkspaceEntry = WorkspaceDisplayEntry;

const reservedRootNames = new Set([
	'.env',
	'.env.git',
	'project.env',
	'compose.yaml',
	'compose.yml',
	'docker-compose.yaml',
	'docker-compose.yml',
	'podman-compose.yaml',
	'podman-compose.yml',
	'compose.override.yaml',
	'compose.override.yml',
	'docker-compose.override.yaml',
	'docker-compose.override.yml'
]);

export function validateProjectWorkspaceFileName(name: string, parentPath = '', composeFileName = 'compose.yaml'): string | null {
	const normalized = validateWorkspaceFileName(name);
	if (!normalized) return null;
	if (!parentPath && isReservedProjectFileName(normalized, composeFileName)) return null;
	return normalized;
}

export function isReservedProjectFileName(name: string, composeFileName = 'compose.yaml'): boolean {
	const lower = name.trim().toLowerCase();
	return lower === composeFileName.toLowerCase() || reservedRootNames.has(lower);
}

function requireValidProjectWorkspaceFileName(name: string, parentPath: string, composeFileName: string): string | null {
	const normalized = validateProjectWorkspaceFileName(name, parentPath, composeFileName);
	if (!normalized) toast.error(m.workspace_file_invalid_name());
	return normalized;
}

export function planProjectWorkspaceFileCreate(
	existingPaths: ReadonlySet<string>,
	parentPath: string,
	name: string,
	composeFileName = 'compose.yaml'
): string | null {
	const normalizedName = requireValidProjectWorkspaceFileName(name, parentPath, composeFileName);
	if (!normalizedName) return null;
	const relativePath = joinWorkspaceFilePath(parentPath, normalizedName);
	if (existingPaths.has(relativePath)) {
		toast.error(m.workspace_file_duplicate_name());
		return null;
	}
	return relativePath;
}

export function planProjectWorkspaceFileRename(
	existingPaths: ReadonlySet<string>,
	relativePath: string,
	newName: string,
	composeFileName = 'compose.yaml'
): { newName: string; newPath: string } | null {
	const parentPath = workspaceFileParentPath(relativePath);
	const normalizedName = requireValidProjectWorkspaceFileName(newName, parentPath, composeFileName);
	if (!normalizedName) return null;
	const newPath = joinWorkspaceFilePath(parentPath, normalizedName);
	if (newPath !== relativePath && existingPaths.has(newPath)) {
		toast.error(m.workspace_file_duplicate_name());
		return null;
	}
	return { newName: normalizedName, newPath };
}
