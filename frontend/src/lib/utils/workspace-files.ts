import type { CodeLanguage } from '#lib/components/code-editor/analysis/types.js';
import { m } from '#lib/paraglide/messages.js';
import { toast } from 'svelte-sonner';
import type {
	WorkspaceFileChange as WorkspaceFileChangeDto,
	WorkspaceFileChangeOperation,
	WorkspaceFileEntry as WorkspaceFileDto,
	WorkspaceReadOnlyReason
} from '#lib/types/workspace.js';

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
	loadedFileContents: Record<string, string>,
	stagedFiles: Record<string, File> = {},
	stagedUploadedText: Record<string, string> = {}
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

	// Stage the picked file verbatim while its content is untouched, so any
	// file — including binary — is stored byte-for-byte as uploaded. Text
	// uploads are compared against their own decoded bytes; a staged binary
	// replacement is always authoritative.
	const stageStagedFile = (change: T): boolean => {
		const staged = stagedFiles[change.relativePath];
		if (!staged) return false;
		const uploaded = stagedUploadedText[change.relativePath];
		if (uploaded !== undefined) {
			const current = fileContents[change.relativePath];
			if (current !== undefined && current !== uploaded) return false;
		}
		change.uploadIndex = files.length;
		files.push(staged);
		return true;
	};

	for (const change of fileChanges) {
		if ((change.operation === 'create_file' || change.operation === 'update_file') && change.uploadIndex === undefined) {
			stageStagedFile(change);
		}
	}

	for (const change of fileChanges) {
		if (change.operation === 'create_file' && change.uploadIndex === undefined) {
			stageText(change, fileContents[change.relativePath] ?? '');
		}
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

	for (const change of fileChanges) {
		if (change.operation === 'update_file' && change.uploadIndex === undefined) stageStagedFile(change);
	}

	// Every update also carries the content the draft was based on, so the
	// backend can reject a save that would overwrite a concurrent external
	// edit to the same file.
	appendWorkspaceBaselines(fileChanges, loadedFileContents, files);

	return { fileChanges, files };
}

function appendWorkspaceBaselines<T extends WorkspaceDisplayFileChange>(
	fileChanges: T[],
	loadedFileContents: Record<string, string>,
	files: File[]
) {
	for (const change of fileChanges) {
		if (change.operation !== 'update_file') continue;
		const baseline = loadedFileContents[change.relativePath];
		if (baseline === undefined) continue;
		change.baselineIndex = files.length;
		files.push(new File([baseline], workspaceFileBasename(change.relativePath), { type: 'text/plain' }));
	}
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

export async function readWorkspaceUpload(
	file: File,
	maxFileSizeMb: number
): Promise<{ content?: string; binary: boolean; error?: string }> {
	if (file.size > maxFileSizeMb * 1024 * 1024) return { binary: false, error: m.workspace_upload_too_large({ maxFileSizeMb }) };
	try {
		const bytes = new Uint8Array(await file.arrayBuffer());
		if (bytes.includes(0)) return { binary: true };
		return { binary: false, content: new TextDecoder('utf-8', { fatal: true }).decode(bytes) };
	} catch {
		return { binary: true };
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

const WORKSPACE_LANGUAGE_EXTENSIONS: Partial<Record<string, CodeLanguage>> = {
	yaml: 'yaml',
	yml: 'yaml',
	json: 'json',
	toml: 'toml',
	dockerfile: 'dockerfile',
	sh: 'shell',
	bash: 'shell',
	zsh: 'shell',
	fish: 'shell',
	ts: 'typescript',
	tsx: 'typescript',
	mts: 'typescript',
	cts: 'typescript',
	js: 'javascript',
	jsx: 'javascript',
	mjs: 'javascript',
	cjs: 'javascript',
	md: 'markdown',
	markdown: 'markdown',
	mdx: 'markdown'
};

export function workspaceFileLanguage(relativePath: string): CodeLanguage {
	const lower = relativePath.toLowerCase();
	const basename = workspaceFileBasename(lower);
	if (lower.endsWith('.env') || basename.startsWith('.env')) return 'env';
	const extension = lower.slice(lower.lastIndexOf('.') + 1);
	// These formats take precedence over the Dockerfile basename convention.
	if (['yaml', 'yml', 'json', 'toml'].includes(extension) && lower.includes('.')) {
		return WORKSPACE_LANGUAGE_EXTENSIONS[extension] ?? 'plaintext';
	}
	if (basename === 'dockerfile' || basename.startsWith('dockerfile.')) return 'dockerfile';
	return lower.includes('.') && Object.hasOwn(WORKSPACE_LANGUAGE_EXTENSIONS, extension)
		? (WORKSPACE_LANGUAGE_EXTENSIONS[extension] ?? 'plaintext')
		: 'plaintext';
}

export function compareWorkspaceFileEntries(a: WorkspaceDisplayEntry, b: WorkspaceDisplayEntry): number {
	if (a.isDirectory !== b.isDirectory) return a.isDirectory ? -1 : 1;
	const baseComparison = a.name.localeCompare(b.name, undefined, { sensitivity: 'base' });
	return baseComparison || a.name.localeCompare(b.name);
}

function remapWorkspaceDisplayEntries(entries: Map<string, WorkspaceDisplayEntry>, relativePath: string, newPath: string) {
	const remapped = new Map<string, WorkspaceDisplayEntry>();
	for (const [entryPath, current] of entries.entries()) {
		if (!workspaceFilePathMatches(entryPath, relativePath)) {
			remapped.set(entryPath, current);
			continue;
		}
		const movedPath = remapWorkspaceFilePath(entryPath, relativePath, newPath);
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
}

export function applyWorkspaceFileChangesForDisplay(
	files: WorkspaceDisplayEntry[],
	changes: WorkspaceDisplayFileChange[]
): WorkspaceDisplayEntry[] {
	const entries = new Map<string, WorkspaceDisplayEntry>(
		files.map((file) => {
			const modeType = file.mode?.[0];
			return [
				file.relativePath,
				{
					...file,
					name: file.name || workspaceFileBasename(file.relativePath),
					isDirectory: modeType ? modeType === 'd' : file.isDirectory,
					isSymlink: modeType ? modeType === 'l' : file.isSymlink
				}
			] as const;
		})
	);

	for (const change of changes) {
		const relativePath = normalizeWorkspaceRelativePath(change.relativePath);
		switch (change.operation) {
			case 'create_file':
			case 'create_folder':
				entries.set(relativePath, {
					path: relativePath,
					relativePath,
					name: workspaceFileBasename(relativePath),
					isDirectory: change.operation === 'create_folder',
					size: 0,
					pending: true
				});
				break;
			case 'restore_file':
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
				remapWorkspaceDisplayEntries(entries, relativePath, newPath);
				break;
			}
			case 'delete':
				for (const entryPath of [...entries.keys()]) {
					if (workspaceFilePathMatches(entryPath, relativePath)) entries.delete(entryPath);
				}
				break;
		}
	}

	return [...entries.values()].sort((a, b) => a.relativePath.localeCompare(b.relativePath));
}
