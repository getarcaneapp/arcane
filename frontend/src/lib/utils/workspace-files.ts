import type { CodeLanguage } from '#lib/components/code-editor/analysis/types';
import { m } from '#lib/paraglide/messages';
import { toast } from 'svelte-sonner';
import type {
	WorkspaceFileChange as WorkspaceFileChangeDto,
	WorkspaceFileChangeOperation,
	WorkspaceFileEntry as WorkspaceFileDto,
	WorkspaceReadOnlyReason
} from '#lib/types/workspace';

export interface WorkspaceDisplayEntry extends Omit<WorkspaceFileDto, 'editable' | 'isSymlink' | 'readOnlyReason'> {
	editable?: boolean;
	isSymlink?: boolean;
	readOnlyReason?: WorkspaceReadOnlyReason | 'restore_pending';
	content?: string;
	mimeType?: string;
	protected?: boolean;
	locked?: boolean;
	pending?: boolean;
}

type WorkspaceDisplayFileChange = Omit<WorkspaceFileChangeDto, 'operation'> & {
	operation: WorkspaceFileChangeOperation | 'restore_file';
};

export function buildWorkspaceMultipartUpdate<T extends WorkspaceDisplayFileChange>(
	draftChanges: T[],
	fileContents: Record<string, string>,
	loadedFileContents: Record<string, string>
): { fileChanges: T[]; files: File[] } {
	const fileChanges = draftChanges.map(
		({ uploadIndex: _uploadIndex, baselineIndex: _baselineIndex, ...change }) => ({ ...change }) as T
	);
	const files: File[] = [];
	const changedPaths = Object.keys(fileContents).filter(
		(relativePath) => fileContents[relativePath] !== loadedFileContents[relativePath]
	);

	const stageText = (change: T, content: string) => {
		change.uploadIndex = files.length;
		files.push(new File([content], workspaceFileBasename(change.relativePath), { type: 'text/plain' }));
	};

	for (const change of fileChanges) {
		if (change.operation === 'create_file') stageText(change, fileContents[change.relativePath] ?? '');
	}

	for (const relativePath of changedPaths) {
		if (
			fileChanges.some((change) => change.operation === 'delete' && workspaceFilePathMatches(relativePath, change.relativePath))
		) {
			continue;
		}
		let change = fileChanges.findLast(
			(candidate) =>
				(candidate.operation === 'create_file' || candidate.operation === 'update_file') &&
				candidate.relativePath === relativePath
		);
		if (!change) {
			change = { operation: 'update_file', relativePath } as T;
			fileChanges.push(change);
		}
		if (change.uploadIndex === undefined) stageText(change, fileContents[relativePath] ?? '');
	}

	// Every update also carries the content the draft was based on, so the
	// backend can reject a save that would overwrite a concurrent external
	// edit to the same file.
	for (const change of fileChanges) {
		if (change.operation !== 'update_file') continue;
		const baseline = loadedFileContents[change.relativePath];
		if (baseline === undefined) continue;
		change.baselineIndex = files.length;
		files.push(new File([baseline], workspaceFileBasename(change.relativePath), { type: 'text/plain' }));
	}

	return { fileChanges, files };
}

function normalizeWorkspaceRelativePath(relativePath: string): string {
	return relativePath.trim().replaceAll('\\', '/').split('/').filter(Boolean).join('/');
}

export function workspaceFileBasename(relativePath: string): string {
	const normalized = normalizeWorkspaceRelativePath(relativePath);
	const index = normalized.lastIndexOf('/');
	return index >= 0 ? normalized.slice(index + 1) : normalized;
}

export function workspaceFileParentPath(relativePath: string): string {
	const normalized = normalizeWorkspaceRelativePath(relativePath);
	const index = normalized.lastIndexOf('/');
	return index >= 0 ? normalized.slice(0, index) : '';
}

export function joinWorkspaceFilePath(parentPath: string, name: string): string {
	const parent = normalizeWorkspaceRelativePath(parentPath);
	return parent ? `${parent}/${name}` : name;
}

export function validateWorkspaceFileName(name: string): string | null {
	const trimmed = name.trim();
	if (!trimmed || trimmed === '.' || trimmed === '..') return null;
	if (trimmed.includes('/') || trimmed.includes('\\') || trimmed.includes('\0')) return null;
	return trimmed;
}

export function planWorkspaceFileCreate(existingPaths: ReadonlySet<string>, parentPath: string, name: string): string | null {
	const normalizedName = validateWorkspaceFileName(name);
	if (!normalizedName) {
		toast.error(m.workspace_file_invalid_name());
		return null;
	}
	const relativePath = joinWorkspaceFilePath(parentPath, normalizedName);
	if (existingPaths.has(relativePath)) {
		toast.error(m.workspace_file_duplicate_name());
		return null;
	}
	return relativePath;
}

export function planWorkspaceFileRename(
	existingPaths: ReadonlySet<string>,
	relativePath: string,
	newName: string
): { newName: string; newPath: string } | null {
	const normalizedName = validateWorkspaceFileName(newName);
	if (!normalizedName) {
		toast.error(m.workspace_file_invalid_name());
		return null;
	}
	const newPath = joinWorkspaceFilePath(workspaceFileParentPath(relativePath), normalizedName);
	if (newPath !== relativePath && existingPaths.has(newPath)) {
		toast.error(m.workspace_file_duplicate_name());
		return null;
	}
	return { newName: normalizedName, newPath };
}

export function planWorkspaceFileMove(
	entry: Pick<WorkspaceDisplayEntry, 'isDirectory'> | undefined,
	existingPaths: ReadonlySet<string>,
	relativePath: string,
	newParentPath: string
): string | null {
	if (!entry || newParentPath === workspaceFileParentPath(relativePath)) return null;
	if (entry.isDirectory && newParentPath && workspaceFilePathMatches(newParentPath, relativePath)) {
		toast.error(m.workspace_file_invalid_move_destination());
		return null;
	}
	const newPath = joinWorkspaceFilePath(newParentPath, workspaceFileBasename(relativePath));
	if (newPath !== relativePath && existingPaths.has(newPath)) {
		toast.error(m.workspace_file_duplicate_name());
		return null;
	}
	return newPath;
}

export async function readWorkspaceTextUpload(file: File, maxFileSizeMb: number): Promise<{ content?: string; error?: string }> {
	if (file.size > maxFileSizeMb * 1024 * 1024) return { error: m.workspace_upload_too_large({ maxFileSizeMb }) };
	try {
		const bytes = new Uint8Array(await file.arrayBuffer());
		if (bytes.includes(0)) return { error: m.workspace_upload_text_required() };
		return { content: new TextDecoder('utf-8', { fatal: true }).decode(bytes) };
	} catch {
		return { error: m.workspace_upload_text_required() };
	}
}

export function workspaceReadOnlyMessage(reason: WorkspaceReadOnlyReason | undefined, maxFileSizeMb: number): string {
	if (reason === 'binary') return m.workspace_binary_readonly();
	if (reason === 'too_large') return m.workspace_large_readonly({ maxFileSizeMb });
	if (reason === 'symlink') return m.workspace_symlink_readonly();
	if (reason === 'special') return m.workspace_special_readonly();
	if (reason === 'gitops_managed') return m.workspace_gitops_readonly();
	return m.workspace_readonly();
}

export function workspaceFilePathMatches(relativePath: string, rootPath: string): boolean {
	return relativePath === rootPath || relativePath.startsWith(`${rootPath}/`);
}

export function remapWorkspaceFilePath(filePath: string, oldPath: string, newPath: string): string {
	return workspaceFilePathMatches(filePath, oldPath) ? `${newPath}${filePath.slice(oldPath.length)}` : filePath;
}

export function remapSelectedWorkspaceFileKey(selectedKey: string, oldPath: string, newPath: string): string | null {
	if (!selectedKey.startsWith('file:')) return null;
	const selectedPath = selectedKey.slice(5);
	if (!workspaceFilePathMatches(selectedPath, oldPath)) return null;
	return `file:${newPath}${selectedPath.slice(oldPath.length)}`;
}

export function isWorkspaceFileSelectionUnder(selectedKey: string, rootPath: string): boolean {
	return selectedKey.startsWith('file:') && workspaceFilePathMatches(selectedKey.slice(5), rootPath);
}

export function remapWorkspaceFileRecord<T>(record: Record<string, T>, oldPath: string, newPath: string): Record<string, T> {
	return Object.fromEntries(
		Object.entries(record).map(([relativePath, value]) => [remapWorkspaceFilePath(relativePath, oldPath, newPath), value])
	);
}

export function removeWorkspaceFileRecord<T>(record: Record<string, T>, rootPath: string): Record<string, T> {
	return Object.fromEntries(Object.entries(record).filter(([relativePath]) => !workspaceFilePathMatches(relativePath, rootPath)));
}

export function workspaceFileLanguage(relativePath: string): CodeLanguage {
	const lower = relativePath.toLowerCase();
	const basename = workspaceFileBasename(lower);
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

export function compareWorkspaceFileEntries(a: WorkspaceDisplayEntry, b: WorkspaceDisplayEntry): number {
	if (a.isDirectory !== b.isDirectory) return a.isDirectory ? -1 : 1;
	const baseComparison = a.name.localeCompare(b.name, undefined, { sensitivity: 'base' });
	return baseComparison || a.name.localeCompare(b.name);
}

export function applyWorkspaceFileChangesForDisplay(
	files: WorkspaceDisplayEntry[],
	changes: WorkspaceDisplayFileChange[]
): WorkspaceDisplayEntry[] {
	const entries = new Map<string, WorkspaceDisplayEntry>();

	for (const file of files) {
		const modeType = file.mode?.[0];
		const isDirectory = modeType ? modeType === 'd' : file.isDirectory;
		const isSymlink = modeType ? modeType === 'l' : file.isSymlink;
		entries.set(file.relativePath, {
			...file,
			name: file.name || workspaceFileBasename(file.relativePath),
			isDirectory,
			isSymlink
		});
	}

	for (const change of changes) {
		const relativePath = normalizeWorkspaceRelativePath(change.relativePath);
		switch (change.operation) {
			case 'create_file':
				entries.set(relativePath, {
					path: relativePath,
					relativePath,
					name: workspaceFileBasename(relativePath),
					isDirectory: false,
					size: 0,
					pending: true
				});
				break;
			case 'create_folder':
				entries.set(relativePath, {
					path: relativePath,
					relativePath,
					name: workspaceFileBasename(relativePath),
					isDirectory: true,
					size: 0,
					pending: true
				});
				break;
			case 'update_file': {
				const entry = entries.get(relativePath);
				if (entry) entries.set(relativePath, { ...entry, pending: true });
				break;
			}
			case 'rename':
			case 'move': {
				const entry = entries.get(relativePath);
				if (!entry) break;
				const newPath =
					change.operation === 'rename' && change.newName
						? joinWorkspaceFilePath(workspaceFileParentPath(relativePath), change.newName)
						: joinWorkspaceFilePath(change.newParentPath ?? '', workspaceFileBasename(relativePath));
				const remapped = new Map<string, WorkspaceDisplayEntry>();
				for (const [entryPath, current] of entries.entries()) {
					if (!workspaceFilePathMatches(entryPath, relativePath)) {
						remapped.set(entryPath, current);
						continue;
					}
					const movedPath = `${newPath}${entryPath.slice(relativePath.length)}`;
					remapped.set(movedPath, {
						...current,
						path: movedPath,
						relativePath: movedPath,
						name: workspaceFileBasename(movedPath),
						pending: true
					});
				}
				entries.clear();
				for (const [entryPath, current] of remapped.entries()) entries.set(entryPath, current);
				break;
			}
			case 'delete':
				for (const entryPath of [...entries.keys()]) {
					if (workspaceFilePathMatches(entryPath, relativePath)) entries.delete(entryPath);
				}
				break;
			case 'restore_file': {
				const entry = entries.get(relativePath);
				if (entry) entries.set(relativePath, { ...entry, pending: true });
				break;
			}
		}
	}

	return [...entries.values()].sort((a, b) => a.relativePath.localeCompare(b.relativePath));
}
