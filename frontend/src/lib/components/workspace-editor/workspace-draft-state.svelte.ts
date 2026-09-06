import type { WorkspaceFileDraft } from '#lib/types/workspace.js';
import { m } from '#lib/paraglide/messages.js';
import {
	isWorkspaceFileSelectionUnder,
	readWorkspaceUpload,
	remapSelectedWorkspaceFileKey,
	remapWorkspaceFilePath,
	remapWorkspaceFileRecord,
	removeWorkspaceFileRecord,
	workspaceFileBasename,
	workspaceFilePathMatches,
	type WorkspaceDisplayEntry
} from '#lib/utils/workspace-files.js';

type RenamePlan = { newPath: string };

type WorkspaceDraftStateOptions = {
	initialFiles?: WorkspaceFileDraft[];
	initialContents?: Record<string, string>;
	initialErrors?: Record<string, boolean>;
	initialValidationReady?: Record<string, boolean>;
	initialOpenTabs?: string[];
	initialSelection?: string;
	pendingEntries?: boolean;
	allowBinary?: boolean;
	fallbackTab: string;
	isFixedTab: (key: string) => boolean;
	isTabAvailable?: (key: string) => boolean;
	planCreate: (existingPaths: ReadonlySet<string>, parentPath: string, name: string) => string | null;
	planRename: (existingPaths: ReadonlySet<string>, relativePath: string, newName: string) => RenamePlan | null;
	planMove: (
		entry: Pick<WorkspaceDisplayEntry, 'isDirectory'> | undefined,
		existingPaths: ReadonlySet<string>,
		relativePath: string,
		newParentPath: string
	) => string | null;
};

export class WorkspaceDraftState {
	files = $state<WorkspaceFileDraft[]>([]);
	contents = $state<Record<string, string>>({});
	stagedFiles = $state<Record<string, File>>({});
	binaryFiles = $state<Record<string, boolean>>({});
	uploadedText = $state<Record<string, string>>({});
	hasErrors = $state<Record<string, boolean>>({});
	validationReady = $state<Record<string, boolean>>({});
	openTabs = $state<string[]>([]);
	selectedKey = $state('');

	readonly #fallbackTab: string;
	readonly #pendingEntries: boolean;
	readonly #allowBinary: boolean;
	readonly #isFixedTab: (key: string) => boolean;
	readonly #isTabAvailable: (key: string) => boolean;
	readonly #planCreate: WorkspaceDraftStateOptions['planCreate'];
	readonly #planRename: WorkspaceDraftStateOptions['planRename'];
	readonly #planMove: WorkspaceDraftStateOptions['planMove'];

	constructor(options: WorkspaceDraftStateOptions) {
		this.files = options.initialFiles?.map((file) => ({ ...file })) ?? [];
		this.contents = { ...options.initialContents };
		this.hasErrors = { ...options.initialErrors };
		this.validationReady = { ...options.initialValidationReady };
		this.openTabs = [...(options.initialOpenTabs ?? [options.fallbackTab])];
		this.selectedKey = options.initialSelection ?? options.fallbackTab;
		this.#fallbackTab = options.fallbackTab;
		this.#pendingEntries = options.pendingEntries ?? true;
		this.#allowBinary = options.allowBinary ?? true;
		this.#isFixedTab = options.isFixedTab;
		this.#isTabAvailable = options.isTabAvailable ?? (() => true);
		this.#planCreate = options.planCreate;
		this.#planRename = options.planRename;
		this.#planMove = options.planMove;
	}

	get entries(): WorkspaceDisplayEntry[] {
		return this.files.map((file) => {
			const staged = this.stagedFiles[file.relativePath];
			const binary = this.binaryFiles[file.relativePath] === true;
			return {
				path: file.relativePath,
				relativePath: file.relativePath,
				name: workspaceFileBasename(file.relativePath),
				isDirectory: !!file.isDirectory,
				size: file.isDirectory ? 0 : binary ? (staged?.size ?? 0) : (this.contents[file.relativePath]?.length ?? 0),
				content: file.isDirectory ? undefined : binary ? undefined : (this.contents[file.relativePath] ?? ''),
				mimeType: file.isDirectory ? undefined : (staged?.type ?? undefined),
				editable: binary ? false : undefined,
				readOnlyReason: binary ? 'binary' : undefined,
				pending: this.#pendingEntries
			};
		});
	}

	get paths(): ReadonlySet<string> {
		return new Set(this.entries.map((file) => file.relativePath));
	}

	get validOpenTabs(): string[] {
		const valid = this.openTabs.filter((key) => {
			if (this.#isFixedTab(key)) return this.#isTabAvailable(key);
			if (!key.startsWith('file:')) return false;
			const entry = this.entries.find((file) => file.relativePath === key.slice(5));
			return !!entry && !entry.isDirectory;
		});
		return valid.length > 0 ? valid : [this.#fallbackTab];
	}

	get activeTab(): string {
		const tabs = this.validOpenTabs;
		return tabs.includes(this.selectedKey) ? this.selectedKey : (tabs[0] ?? this.#fallbackTab);
	}

	isDirectoryKey(key: string): boolean {
		if (!key.startsWith('file:')) return false;
		return this.entries.find((file) => file.relativePath === key.slice(5))?.isDirectory === true;
	}

	openTab = (key: string) => {
		if (!this.isDirectoryKey(key) && !this.openTabs.includes(key)) {
			this.openTabs = [...this.openTabs, key];
		}
		this.selectedKey = key;
	};

	closeTab = (key: string) => {
		const tabs = this.validOpenTabs;
		const index = tabs.indexOf(key);
		const remaining = tabs.filter((tab) => tab !== key);
		this.openTabs = this.openTabs.filter((tab) => tab !== key);
		if (this.selectedKey === key) {
			this.selectedKey = remaining[Math.min(Math.max(index - 1, 0), remaining.length - 1)] ?? this.#fallbackTab;
		}
	};

	createFile = (parentPath: string, name: string, content = ''): string | null => {
		const relativePath = this.#planCreate(this.paths, parentPath, name);
		if (!relativePath) return null;
		this.files = [...this.files, { relativePath, isDirectory: false }];
		this.contents = { ...this.contents, [relativePath]: content };
		this.ensureValidationState(relativePath);
		this.openTab(`file:${relativePath}`);
		return relativePath;
	};

	createFolder = (parentPath: string, name: string): string | null => {
		const relativePath = this.#planCreate(this.paths, parentPath, name);
		if (!relativePath) return null;
		this.files = [...this.files, { relativePath, isDirectory: true }];
		this.selectedKey = `file:${relativePath}`;
		return relativePath;
	};

	uploadFile = async (parentPath: string, files: File[], maxFileSizeMb: number): Promise<string | void> => {
		const file = files[0];
		if (!file) return m.workspace_upload_file_required();
		const result = await readWorkspaceUpload(file, maxFileSizeMb);
		if (result.error) return result.error;
		const isBinary = result.binary === true;
		if (isBinary && !this.#allowBinary) return m.workspace_upload_text_required();
		const relativePath = this.#planCreate(this.paths, parentPath, file.name);
		if (!relativePath) return;
		this.files = [...this.files, { relativePath, isDirectory: false }];
		this.stagedFiles = { ...this.stagedFiles, [relativePath]: file };
		if (isBinary) {
			this.binaryFiles = { ...this.binaryFiles, [relativePath]: true };
		} else {
			const content = result.content ?? '';
			this.contents = { ...this.contents, [relativePath]: content };
			this.uploadedText = { ...this.uploadedText, [relativePath]: content };
		}
		this.openTab(`file:${relativePath}`);
	};

	rename = (relativePath: string, newName: string): string | null => {
		const plan = this.#planRename(this.paths, relativePath, newName);
		if (!plan) return null;
		this.applyPathChange(relativePath, plan.newPath);
		return plan.newPath;
	};

	move = (relativePath: string, newParentPath: string): string | null => {
		const entry = this.entries.find((file) => file.relativePath === relativePath);
		const newPath = this.#planMove(entry, this.paths, relativePath, newParentPath);
		if (!newPath) return null;
		this.applyPathChange(relativePath, newPath);
		return newPath;
	};

	remove = (relativePath: string) => {
		this.files = this.files.filter((file) => !workspaceFilePathMatches(file.relativePath, relativePath));
		this.contents = removeWorkspaceFileRecord(this.contents, relativePath);
		this.stagedFiles = removeWorkspaceFileRecord(this.stagedFiles, relativePath);
		this.binaryFiles = removeWorkspaceFileRecord(this.binaryFiles, relativePath);
		this.uploadedText = removeWorkspaceFileRecord(this.uploadedText, relativePath);
		this.hasErrors = removeWorkspaceFileRecord(this.hasErrors, relativePath);
		this.validationReady = removeWorkspaceFileRecord(this.validationReady, relativePath);
		this.openTabs = this.openTabs.filter((tab) => !isWorkspaceFileSelectionUnder(tab, relativePath));
		if (isWorkspaceFileSelectionUnder(this.selectedKey, relativePath)) {
			this.selectedKey = this.validOpenTabs[0] ?? this.#fallbackTab;
		}
	};

	toDrafts(): WorkspaceFileDraft[] {
		return this.files.map((file) => {
			const relativePath = file.relativePath;
			const staged = this.stagedFiles[relativePath];
			const binary = this.binaryFiles[relativePath] === true;
			const uploadedText = this.uploadedText[relativePath];
			const untouched = uploadedText !== undefined && this.contents[relativePath] === uploadedText;
			return {
				relativePath,
				isDirectory: !!file.isDirectory,
				content: file.isDirectory ? undefined : binary ? undefined : (this.contents[relativePath] ?? ''),
				file: file.isDirectory ? undefined : binary || untouched ? staged : undefined
			};
		});
	}

	private ensureValidationState(relativePath: string) {
		if (this.hasErrors[relativePath] === undefined) {
			this.hasErrors = { ...this.hasErrors, [relativePath]: false };
		}
		if (this.validationReady[relativePath] === undefined) {
			this.validationReady = { ...this.validationReady, [relativePath]: true };
		}
	}

	private applyPathChange(oldPath: string, newPath: string) {
		this.files = this.files.map((file) => ({
			...file,
			relativePath: remapWorkspaceFilePath(file.relativePath, oldPath, newPath)
		}));
		this.contents = remapWorkspaceFileRecord(this.contents, oldPath, newPath);
		this.stagedFiles = remapWorkspaceFileRecord(this.stagedFiles, oldPath, newPath);
		this.binaryFiles = remapWorkspaceFileRecord(this.binaryFiles, oldPath, newPath);
		this.uploadedText = remapWorkspaceFileRecord(this.uploadedText, oldPath, newPath);
		this.hasErrors = remapWorkspaceFileRecord(this.hasErrors, oldPath, newPath);
		this.validationReady = remapWorkspaceFileRecord(this.validationReady, oldPath, newPath);
		this.openTabs = this.openTabs.map((tab) => remapSelectedWorkspaceFileKey(tab, oldPath, newPath) ?? tab);
		this.selectedKey = remapSelectedWorkspaceFileKey(this.selectedKey, oldPath, newPath) ?? this.selectedKey;
	}
}
