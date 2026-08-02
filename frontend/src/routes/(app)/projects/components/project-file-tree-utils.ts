import { toast } from 'svelte-sonner';
import { m } from '#lib/paraglide/messages';
import {
	joinWorkspaceFilePath,
	validateWorkspaceFileName,
	workspaceFileBasename,
	workspaceFileParentPath,
	workspaceFilePathMatches,
	type WorkspaceFileEntry
} from '#lib/utils/workspace-files';

export type ManagedProjectFileEntry = WorkspaceFileEntry;

const maxManagedProjectFileBytes = 1024 * 1024;

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

export function validateProjectFileName(name: string, parentPath = '', composeFileName = 'compose.yaml'): string | null {
	const normalized = validateWorkspaceFileName(name);
	if (!normalized) return null;
	if (!parentPath && isReservedProjectFileName(normalized, composeFileName)) return null;
	return normalized;
}

export function isReservedProjectFileName(name: string, composeFileName = 'compose.yaml'): boolean {
	const lower = name.trim().toLowerCase();
	return lower === composeFileName.toLowerCase() || reservedRootNames.has(lower);
}

function requireValidProjectFileName(name: string, parentPath: string, composeFileName: string): string | null {
	const normalized = validateProjectFileName(name, parentPath, composeFileName);
	if (!normalized) toast.error(m.project_file_invalid_name());
	return normalized;
}

export function planProjectFileCreate(
	existingPaths: ReadonlySet<string>,
	parentPath: string,
	name: string,
	composeFileName = 'compose.yaml'
): string | null {
	const normalizedName = requireValidProjectFileName(name, parentPath, composeFileName);
	if (!normalizedName) return null;
	const relativePath = joinWorkspaceFilePath(parentPath, normalizedName);
	if (existingPaths.has(relativePath)) {
		toast.error(m.project_file_duplicate_name());
		return null;
	}
	return relativePath;
}

export function planProjectFileRename(
	existingPaths: ReadonlySet<string>,
	relativePath: string,
	newName: string,
	composeFileName = 'compose.yaml'
): { newName: string; newPath: string } | null {
	const parentPath = workspaceFileParentPath(relativePath);
	const normalizedName = requireValidProjectFileName(newName, parentPath, composeFileName);
	if (!normalizedName) return null;
	const newPath = joinWorkspaceFilePath(parentPath, normalizedName);
	if (newPath !== relativePath && existingPaths.has(newPath)) {
		toast.error(m.project_file_duplicate_name());
		return null;
	}
	return { newName: normalizedName, newPath };
}

export function planProjectFileMove(
	entry: Pick<WorkspaceFileEntry, 'isDirectory'> | undefined,
	existingPaths: ReadonlySet<string>,
	relativePath: string,
	newParentPath: string
): string | null {
	if (!entry) return null;
	if (newParentPath === workspaceFileParentPath(relativePath)) return null;
	if (entry.isDirectory && newParentPath && workspaceFilePathMatches(newParentPath, relativePath)) {
		toast.error(m.project_file_invalid_move_destination());
		return null;
	}
	const newPath = joinWorkspaceFilePath(newParentPath, workspaceFileBasename(relativePath));
	if (newPath !== relativePath && existingPaths.has(newPath)) {
		toast.error(m.project_file_duplicate_name());
		return null;
	}
	return newPath;
}

export async function readProjectTextUpload(file: File): Promise<{ content?: string; error?: string }> {
	if (file.size > maxManagedProjectFileBytes) return { error: m.project_file_upload_too_large() };

	try {
		const bytes = new Uint8Array(await file.arrayBuffer());
		if (bytes.includes(0)) return { error: m.project_file_upload_text_required() };
		return { content: new TextDecoder('utf-8', { fatal: true }).decode(bytes) };
	} catch {
		return { error: m.project_file_upload_text_required() };
	}
}
