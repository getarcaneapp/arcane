import { toast } from 'svelte-sonner';
import { SvelteMap } from 'svelte/reactivity';

import { m } from '#lib/paraglide/messages.js';
import { buildWorkspaceService } from '#lib/services/build-workspace-service.js';
import type { Workspace, WorkspaceFileChange, WorkspaceFileContent } from '#lib/types/workspace.js';
import { extractApiErrorMessage, handleApiResultWithCallbacks } from '#lib/utils/api.js';
import { tryCatch } from '#lib/utils/try-catch.js';
import {
	applyWorkspaceFileChangesForDisplay,
	buildWorkspaceMultipartUpdate,
	isWorkspaceFileSelectionUnder,
	joinWorkspaceFilePath,
	planWorkspaceFileCreate,
	planWorkspaceFileMove,
	planWorkspaceFileRename,
	remapSelectedWorkspaceFileKey,
	remapWorkspaceFilePath,
	remapWorkspaceFileRecord,
	removeWorkspaceFileRecord,
	validateWorkspaceFileName,
	workspaceFileBasename,
	workspaceFileParentPath,
	workspaceFilePathMatches
} from '#lib/utils/workspace-files.js';

type BuildWorkspaceDraftOptions = {
	environmentId: string;
	workspace: () => Workspace | undefined;
	onWorkspace: (workspace: Workspace) => void;
};

export class BuildWorkspaceDraft {
	changes = $state<WorkspaceFileChange[]>([]);
	contents = $state<Record<string, string>>({});
	loadedContents = $state<Record<string, string>>({});
	metadata = $state<Record<string, WorkspaceFileContent>>({});
	loadErrors = $state<Record<string, string>>({});
	loading = $state<Record<string, boolean>>({});
	openTabs = $state<string[]>([]);
	selectedKey = $state('');
	contextPath = $state('');
	saving = $state(false);

	readonly environmentId: string;
	readonly #options: BuildWorkspaceDraftOptions;
	readonly #loadTokens = new SvelteMap<string, number>();

	entries = $derived.by(() =>
		applyWorkspaceFileChangesForDisplay(this.#options.workspace()?.files ?? [], this.changes).map((file) => {
			const modeType = file.mode?.[0];
			const metadata = this.metadata[file.relativePath];
			const editable = metadata?.editable ?? file.editable;
			return {
				...file,
				editable,
				readOnlyReason: metadata?.readOnlyReason ?? file.readOnlyReason,
				locked:
					!file.isDirectory &&
					(editable === false || file.isSymlink === true || (!!modeType && modeType !== '-' && modeType !== 'd'))
			};
		})
	);
	paths = $derived(new Set(this.entries.map((entry) => entry.relativePath)));
	changedPaths = $derived(
		Object.keys(this.contents).filter((relativePath) => this.contents[relativePath] !== this.loadedContents[relativePath])
	);
	hasChanges = $derived(this.changes.length > 0 || this.changedPaths.length > 0);
	fileTabs = $derived(
		this.openTabs.filter((key) => {
			if (!key.startsWith('file:')) return false;
			const entry = this.entries.find((file) => file.relativePath === key.slice(5));
			return !!entry && !entry.isDirectory;
		})
	);
	activeTab = $derived(this.fileTabs.includes(this.selectedKey) ? this.selectedKey : '');

	constructor(options: BuildWorkspaceDraftOptions) {
		this.environmentId = options.environmentId;
		this.#options = options;
	}

	openTab = (key: string) => {
		if (!this.openTabs.includes(key)) this.openTabs = [...this.openTabs, key];
		this.selectedKey = key;
		this.loadSelected();
	};

	closeTab = (key: string) => {
		const index = this.fileTabs.indexOf(key);
		const remaining = this.fileTabs.filter((tab) => tab !== key);
		this.openTabs = this.openTabs.filter((tab) => tab !== key);
		if (this.activeTab === key) this.selectedKey = remaining[Math.max(index - 1, 0)] ?? '';
		this.loadSelected();
	};

	loadSelected() {
		if (!this.activeTab.startsWith('file:')) return;
		const relativePath = this.activeTab.slice(5);
		if (this.metadata[relativePath] || this.loading[relativePath] || this.loadErrors[relativePath] !== undefined) return;
		void this.#load(relativePath);
	}

	async #load(relativePath: string) {
		const entry = this.entries.find((file) => file.relativePath === relativePath);
		if (!entry || entry.isDirectory) return;
		if (entry.isSymlink) {
			this.metadata = {
				...this.metadata,
				[relativePath]: {
					path: entry.path,
					relativePath,
					name: entry.name,
					size: entry.size,
					mimeType: '',
					editable: false,
					readOnlyReason: 'symlink'
				}
			};
			return;
		}
		const token = (this.#loadTokens.get(relativePath) ?? 0) + 1;
		this.#loadTokens.set(relativePath, token);
		this.loading = { ...this.loading, [relativePath]: true };
		const result = await tryCatch(buildWorkspaceService.getWorkspaceFile(relativePath, this.environmentId));
		if (this.#loadTokens.get(relativePath) !== token) return;
		this.#loadTokens.delete(relativePath);
		this.loading = removeWorkspaceFileRecord(this.loading, relativePath);
		if (result.error !== null) {
			this.loadErrors = { ...this.loadErrors, [relativePath]: extractApiErrorMessage(result.error) };
			return;
		}
		this.metadata = { ...this.metadata, [relativePath]: result.data };
		if (!result.data.editable) return;
		const content = result.data.content ?? '';
		this.loadedContents = { ...this.loadedContents, [relativePath]: content };
		if (this.contents[relativePath] === undefined) this.contents = { ...this.contents, [relativePath]: content };
	}

	createFile = (parentPath: string, name: string) => {
		const relativePath = planWorkspaceFileCreate(this.paths, parentPath, name);
		if (!relativePath) return;
		this.changes = [...this.changes, { operation: 'create_file', relativePath }];
		this.contents = { ...this.contents, [relativePath]: '' };
		this.loadedContents = { ...this.loadedContents, [relativePath]: '' };
		this.metadata = {
			...this.metadata,
			[relativePath]: {
				path: relativePath,
				relativePath,
				name: workspaceFileBasename(relativePath),
				size: 0,
				mimeType: 'text/plain',
				content: '',
				editable: true
			}
		};
		this.openTab(`file:${relativePath}`);
	};

	createFolder = (parentPath: string, name: string) => {
		const relativePath = planWorkspaceFileCreate(this.paths, parentPath, name);
		if (!relativePath) return;
		this.changes = [...this.changes, { operation: 'create_folder', relativePath }];
	};

	upload = async (parentPath: string, files: File[]): Promise<string | void> => {
		if (files.some((file) => !validateWorkspaceFileName(file.name))) return m.workspace_file_invalid_name();
		const baseline = this.#options.workspace()?.files ?? [];
		const baselinePaths = new Set(baseline.map((file) => file.relativePath));
		if (
			files.some((file) => {
				const relativePath = joinWorkspaceFilePath(parentPath, file.name);
				return this.paths.has(relativePath) || baselinePaths.has(relativePath);
			})
		) {
			return m.workspace_file_duplicate_name();
		}
		const baselineFolders = new Set(baseline.filter((file) => file.isDirectory).map((file) => file.relativePath));
		const createdFolders = new Set(
			this.changes.filter((change) => change.operation === 'create_folder').map((change) => change.relativePath)
		);
		for (let folder = parentPath; folder; folder = workspaceFileParentPath(folder)) {
			if (!baselineFolders.has(folder) && !createdFolders.has(folder)) return m.workspace_upload_save_pending_first();
		}
		for (const file of files) {
			const result = await tryCatch(buildWorkspaceService.uploadFile(parentPath, file, this.environmentId));
			if (result.error !== null) return extractApiErrorMessage(result.error);
			this.changes = this.changes.filter(
				(change) => change.operation !== 'create_folder' || !workspaceFilePathMatches(parentPath, change.relativePath)
			);
			this.#options.onWorkspace(result.data);
			this.openTab(`file:${joinWorkspaceFilePath(parentPath, file.name)}`);
		}
	};

	rename = (relativePath: string, newName: string) => {
		const plan = planWorkspaceFileRename(this.paths, relativePath, newName);
		if (!plan || plan.newPath === relativePath) return;
		this.changes = [...this.changes, { operation: 'rename', relativePath, newName: plan.newName }];
		this.#remap(relativePath, plan.newPath);
	};

	move = (relativePath: string, newParentPath: string) => {
		const entry = this.entries.find((file) => file.relativePath === relativePath);
		const newPath = planWorkspaceFileMove(entry, this.paths, relativePath, newParentPath);
		if (!newPath) return;
		this.changes = [...this.changes, { operation: 'move', relativePath, newParentPath }];
		this.#remap(relativePath, newPath);
	};

	remove = (relativePath: string) => {
		const entry = this.entries.find((file) => file.relativePath === relativePath);
		if (!entry) return;
		const touchedByPathChange = this.changes.some(
			(change) =>
				(change.operation === 'rename' || change.operation === 'move') &&
				(entry.pending || workspaceFilePathMatches(change.relativePath, relativePath))
		);
		const insideCreated = this.changes.some(
			(change) =>
				(change.operation === 'create_file' || change.operation === 'create_folder') &&
				workspaceFilePathMatches(relativePath, change.relativePath)
		);
		this.changes =
			insideCreated && !touchedByPathChange
				? this.changes.filter((change) => !workspaceFilePathMatches(change.relativePath, relativePath))
				: [...this.changes, { operation: 'delete', relativePath, recursive: entry.isDirectory }];
		this.#removeState(relativePath);
	};

	download = async (relativePath: string) => {
		const result = await tryCatch(buildWorkspaceService.downloadWorkspaceFile(relativePath, this.environmentId));
		if (result.error !== null) toast.error(m.common_download_error(), { description: extractApiErrorMessage(result.error) });
	};

	save = async (): Promise<boolean> => {
		const workspace = this.#options.workspace();
		if (!this.hasChanges) return true;
		if (!workspace || this.saving) return false;
		const update = buildWorkspaceMultipartUpdate(this.changes, this.contents, this.loadedContents);
		this.saving = true;
		let saved = false;
		await handleApiResultWithCallbacks({
			result: await tryCatch(
				buildWorkspaceService.updateWorkspace(
					{ fileTreeRevision: workspace.fileTreeRevision, fileChanges: update.fileChanges },
					update.files,
					this.environmentId
				)
			),
			message: m.common_save_failed(),
			setLoadingState: (value) => (this.saving = value),
			onSuccess: (next) => {
				this.changes = [];
				this.loadedContents = { ...this.contents };
				this.loadErrors = {};
				this.#options.onWorkspace(next);
				saved = true;
				toast.success(m.workspace_saved());
			}
		});
		return saved;
	};

	#forgetLoads(relativePath: string) {
		for (const path of this.#loadTokens.keys()) {
			if (workspaceFilePathMatches(path, relativePath)) this.#loadTokens.delete(path);
		}
		this.loading = removeWorkspaceFileRecord(this.loading, relativePath);
	}

	#remap(oldPath: string, newPath: string) {
		this.#forgetLoads(oldPath);
		this.contents = remapWorkspaceFileRecord(this.contents, oldPath, newPath);
		this.loadedContents = remapWorkspaceFileRecord(this.loadedContents, oldPath, newPath);
		this.metadata = remapWorkspaceFileRecord(this.metadata, oldPath, newPath);
		this.loadErrors = remapWorkspaceFileRecord(this.loadErrors, oldPath, newPath);
		this.openTabs = this.openTabs.map((tab) => remapSelectedWorkspaceFileKey(tab, oldPath, newPath) ?? tab);
		this.selectedKey = remapSelectedWorkspaceFileKey(this.selectedKey, oldPath, newPath) ?? this.selectedKey;
		this.contextPath = remapWorkspaceFilePath(this.contextPath, oldPath, newPath);
		this.loadSelected();
	}

	#removeState(relativePath: string) {
		this.#forgetLoads(relativePath);
		this.contents = removeWorkspaceFileRecord(this.contents, relativePath);
		this.loadedContents = removeWorkspaceFileRecord(this.loadedContents, relativePath);
		this.metadata = removeWorkspaceFileRecord(this.metadata, relativePath);
		this.loadErrors = removeWorkspaceFileRecord(this.loadErrors, relativePath);
		this.openTabs = this.openTabs.filter((tab) => !isWorkspaceFileSelectionUnder(tab, relativePath));
		if (isWorkspaceFileSelectionUnder(this.selectedKey, relativePath)) this.selectedKey = '';
		if (workspaceFilePathMatches(this.contextPath, relativePath)) this.contextPath = '';
		this.loadSelected();
	}
}
