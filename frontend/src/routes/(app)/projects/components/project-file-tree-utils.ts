import { toast } from 'svelte-sonner';
import { m } from '$lib/paraglide/messages';
import type { ProjectFile, ProjectFileChange } from '$lib/types/project-files';
import type { CodeLanguage } from '$lib/components/code-editor/analysis/types';

export type ManagedProjectFileEntry = ProjectFile & {
	pending?: boolean;
};

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

export function projectFileBasename(relativePath: string): string {
	const normalized = normalizeProjectRelativePath(relativePath);
	const index = normalized.lastIndexOf('/');
	return index >= 0 ? normalized.slice(index + 1) : normalized;
}

export function projectFileParentPath(relativePath: string): string {
	const normalized = normalizeProjectRelativePath(relativePath);
	const index = normalized.lastIndexOf('/');
	return index >= 0 ? normalized.slice(0, index) : '';
}

export function joinProjectFilePath(parentPath: string, name: string): string {
	const parent = normalizeProjectRelativePath(parentPath);
	return parent ? `${parent}/${name}` : name;
}

export function normalizeProjectRelativePath(relativePath: string): string {
	return relativePath.trim().replaceAll('\\', '/').split('/').filter(Boolean).join('/');
}

export function validateProjectFileName(name: string, parentPath = '', composeFileName = 'compose.yaml'): string | null {
	const trimmed = name.trim();
	if (!trimmed || trimmed === '.' || trimmed === '..') return null;
	if (trimmed.includes('/') || trimmed.includes('\\') || trimmed.includes('\0')) return null;
	if (!parentPath && isReservedProjectFileName(trimmed, composeFileName)) return null;
	return trimmed;
}

export function isReservedProjectFileName(name: string, composeFileName = 'compose.yaml'): boolean {
	const lower = name.trim().toLowerCase();
	return lower === composeFileName.toLowerCase() || reservedRootNames.has(lower);
}

export function projectFilePathMatches(relativePath: string, rootPath: string): boolean {
	return relativePath === rootPath || relativePath.startsWith(`${rootPath}/`);
}

function requireValidProjectFileName(name: string, parentPath: string, composeFileName: string): string | null {
	const normalized = validateProjectFileName(name, parentPath, composeFileName);
	if (!normalized) {
		toast.error(m.project_file_invalid_name());
	}
	return normalized;
}

// Validates a create operation and returns the new relative path, or null
// (after showing a toast) when the name is invalid or already taken.
export function planProjectFileCreate(
	existingPaths: ReadonlySet<string>,
	parentPath: string,
	name: string,
	composeFileName = 'compose.yaml'
): string | null {
	const normalizedName = requireValidProjectFileName(name, parentPath, composeFileName);
	if (!normalizedName) return null;
	const relativePath = joinProjectFilePath(parentPath, normalizedName);
	if (existingPaths.has(relativePath)) {
		toast.error(m.project_file_duplicate_name());
		return null;
	}
	return relativePath;
}

// Validates a rename and returns the normalized name plus resulting path, or
// null (after showing a toast) when invalid.
export function planProjectFileRename(
	existingPaths: ReadonlySet<string>,
	relativePath: string,
	newName: string,
	composeFileName = 'compose.yaml'
): { newName: string; newPath: string } | null {
	const parentPath = projectFileParentPath(relativePath);
	const normalizedName = requireValidProjectFileName(newName, parentPath, composeFileName);
	if (!normalizedName) return null;
	const newPath = joinProjectFilePath(parentPath, normalizedName);
	if (newPath !== relativePath && existingPaths.has(newPath)) {
		toast.error(m.project_file_duplicate_name());
		return null;
	}
	return { newName: normalizedName, newPath };
}

// Validates a move and returns the resulting path. Returns null without a
// toast when the move is a no-op, or with a toast when it is invalid.
export function planProjectFileMove(
	entry: Pick<ManagedProjectFileEntry, 'isDirectory'> | undefined,
	existingPaths: ReadonlySet<string>,
	relativePath: string,
	newParentPath: string
): string | null {
	if (!entry) return null;
	if (newParentPath === projectFileParentPath(relativePath)) return null;
	if (entry.isDirectory && newParentPath && projectFilePathMatches(newParentPath, relativePath)) {
		toast.error(m.project_file_invalid_move_destination());
		return null;
	}
	const newPath = joinProjectFilePath(newParentPath, projectFileBasename(relativePath));
	if (newPath !== relativePath && existingPaths.has(newPath)) {
		toast.error(m.project_file_duplicate_name());
		return null;
	}
	return newPath;
}

export function remapProjectFilePath(path: string, oldPath: string, newPath: string): string {
	return projectFilePathMatches(path, oldPath) ? `${newPath}${path.slice(oldPath.length)}` : path;
}

// Remaps a "file:<path>" selection key after a rename/move; returns null when
// the selection is unrelated to oldPath.
export function remapSelectedProjectFileKey(selectedKey: string, oldPath: string, newPath: string): string | null {
	if (!selectedKey.startsWith('file:')) return null;
	const selectedPath = selectedKey.slice(5);
	if (!projectFilePathMatches(selectedPath, oldPath)) return null;
	return `file:${newPath}${selectedPath.slice(oldPath.length)}`;
}

export function isProjectFileSelectionUnder(selectedKey: string, rootPath: string): boolean {
	return selectedKey.startsWith('file:') && projectFilePathMatches(selectedKey.slice(5), rootPath);
}

export function remapProjectFileRecord<T>(record: Record<string, T>, oldPath: string, newPath: string): Record<string, T> {
	return Object.fromEntries(
		Object.entries(record).map(([relativePath, value]) => {
			if (!projectFilePathMatches(relativePath, oldPath)) {
				return [relativePath, value] as const;
			}

			const suffix = relativePath.slice(oldPath.length);
			return [`${newPath}${suffix}`, value] as const;
		})
	);
}

export function removeProjectFileRecord<T>(record: Record<string, T>, rootPath: string): Record<string, T> {
	return Object.fromEntries(Object.entries(record).filter(([relativePath]) => !projectFilePathMatches(relativePath, rootPath)));
}

export function projectFileLanguage(relativePath: string): CodeLanguage {
	const lower = relativePath.toLowerCase();
	const basename = projectFileBasename(lower);
	if (lower.endsWith('.env') || basename.startsWith('.env')) return 'env';
	if (lower.endsWith('.yaml') || lower.endsWith('.yml')) return 'yaml';
	if (lower.endsWith('.json')) return 'json';
	if (lower.endsWith('.toml')) return 'toml';
	if (basename === 'dockerfile' || basename.startsWith('dockerfile.') || lower.endsWith('.dockerfile')) return 'dockerfile';
	if (lower.endsWith('.sh') || lower.endsWith('.bash') || lower.endsWith('.zsh') || lower.endsWith('.fish')) return 'shell';
	if (lower.endsWith('.ts') || lower.endsWith('.tsx') || lower.endsWith('.mts') || lower.endsWith('.cts')) return 'typescript';
	if (lower.endsWith('.js') || lower.endsWith('.jsx') || lower.endsWith('.mjs') || lower.endsWith('.cjs')) return 'javascript';
	if (lower.endsWith('.md') || lower.endsWith('.markdown') || lower.endsWith('.mdx')) return 'markdown';
	return 'plaintext';
}

export function compareProjectFileEntries(a: ManagedProjectFileEntry, b: ManagedProjectFileEntry): number {
	if (a.isDirectory !== b.isDirectory) return a.isDirectory ? -1 : 1;
	return a.name.localeCompare(b.name, undefined, { sensitivity: 'base' });
}

export function applyProjectFileChangesForDisplay(files: ProjectFile[], changes: ProjectFileChange[]): ManagedProjectFileEntry[] {
	const entries = new Map<string, ManagedProjectFileEntry>();

	for (const file of files) {
		entries.set(file.relativePath, {
			...file,
			name: file.name || projectFileBasename(file.relativePath)
		});
	}

	for (const change of changes) {
		const relativePath = normalizeProjectRelativePath(change.relativePath);
		switch (change.operation) {
			case 'create_file':
				entries.set(relativePath, {
					path: relativePath,
					relativePath,
					name: projectFileBasename(relativePath),
					isDirectory: false,
					size: change.content?.length ?? 0,
					pending: true
				});
				break;
			case 'create_folder':
				entries.set(relativePath, {
					path: relativePath,
					relativePath,
					name: projectFileBasename(relativePath),
					isDirectory: true,
					size: 0,
					pending: true
				});
				break;
			case 'rename': {
				const entry = entries.get(relativePath);
				if (!entry || !change.newName) break;

				const parentPath = projectFileParentPath(relativePath);
				const newPath = joinProjectFilePath(parentPath, change.newName);
				const remapped = new Map<string, ManagedProjectFileEntry>();
				for (const [entryPath, current] of entries.entries()) {
					if (!projectFilePathMatches(entryPath, relativePath)) {
						remapped.set(entryPath, current);
						continue;
					}

					const suffix = entryPath.slice(relativePath.length);
					const movedPath = `${newPath}${suffix}`;
					remapped.set(movedPath, {
						...current,
						path: movedPath,
						relativePath: movedPath,
						name: projectFileBasename(movedPath),
						pending: true
					});
				}
				entries.clear();
				for (const [entryPath, current] of remapped.entries()) {
					entries.set(entryPath, current);
				}
				break;
			}
			case 'move': {
				const entry = entries.get(relativePath);
				if (!entry) break;

				const newParentPath = normalizeProjectRelativePath(change.newParentPath ?? '');
				const newPath = joinProjectFilePath(newParentPath, projectFileBasename(relativePath));
				const remapped = new Map<string, ManagedProjectFileEntry>();
				for (const [entryPath, current] of entries.entries()) {
					if (!projectFilePathMatches(entryPath, relativePath)) {
						remapped.set(entryPath, current);
						continue;
					}

					const suffix = entryPath.slice(relativePath.length);
					const movedPath = `${newPath}${suffix}`;
					remapped.set(movedPath, {
						...current,
						path: movedPath,
						relativePath: movedPath,
						name: projectFileBasename(movedPath),
						pending: true
					});
				}
				entries.clear();
				for (const [entryPath, current] of remapped.entries()) {
					entries.set(entryPath, current);
				}
				break;
			}
			case 'delete':
				for (const entryPath of [...entries.keys()]) {
					if (projectFilePathMatches(entryPath, relativePath)) {
						entries.delete(entryPath);
					}
				}
				break;
		}
	}

	return [...entries.values()].sort((a, b) => a.relativePath.localeCompare(b.relativePath));
}
