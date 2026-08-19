<script lang="ts">
	import { untrack } from 'svelte';
	import { ArcaneButton } from '#lib/components/arcane-button/index.js';
	import { openConfirmDialog } from '#lib/components/confirm-dialog';
	import * as Dialog from '#lib/components/ui/dialog';
	import { Input } from '#lib/components/ui/input';
	import { Label } from '#lib/components/ui/label';
	import * as Tooltip from '#lib/components/ui/tooltip/index.js';
	import { ArrowDownIcon, ArrowRightIcon, CreateFileIcon, CreateFolderIcon, FolderOpenIcon, UploadIcon } from '#lib/icons';
	import { m } from '#lib/paraglide/messages';
	import { cn } from '#lib/utils';
	import {
		joinWorkspaceFilePath,
		workspaceFileBasename,
		workspaceFileParentPath,
		workspaceFilePathMatches,
		validateWorkspaceFileName,
		type WorkspaceFileEntry
	} from '#lib/utils/workspace-files';
	import type {
		ContextMenuItem,
		ContextMenuOpenContext,
		FileTree as PierreFileTree,
		FileTreeDirectoryHandle,
		FileTreeDropResult,
		FileTreeRenameEvent,
		GitStatusEntry
	} from '@pierre/trees';

	type DialogMode = 'create_file' | 'create_folder' | 'move' | 'upload';
	type WorkspaceTreeLeadingRow = {
		key: string;
		label: string;
		iconClass?: string;
		locked?: boolean;
		action?: boolean;
		onSelect?: () => void;
	};
	type FolderDestinationOption = {
		relativePath: string;
		label: string;
		depth: number;
		hasChildren: boolean;
		disabled: boolean;
		reason?: string;
	};

	interface Props {
		title?: string;
		leadingRows?: WorkspaceTreeLeadingRow[];
		entries: WorkspaceFileEntry[];
		selectedFile: string;
		disabled?: boolean;
		readOnlyMessage?: string;
		onSelect: (key: string) => void;
		onCreateFile?: (parentPath: string, name: string) => void;
		onCreateFolder?: (parentPath: string, name: string) => void;
		onRename?: (relativePath: string, newName: string) => void;
		onMove?: (relativePath: string, newParentPath: string) => void;
		onDelete?: (relativePath: string) => void;
		onUpload?: (parentPath: string, files: File[]) => Promise<string | void> | string | void;
		onDownload?: (relativePath: string) => void;
		onRestore?: (relativePath: string) => void;
		multipleUploads?: boolean;
		allowUploadOverwrite?: boolean;
		validateName?: (name: string, parentPath: string) => string | null;
		emptyMessage?: string;
		uploadDescription?: string;
		rootDestinationLabel?: string;
		rootPathMessage?: string;
		deleteConfirmMessage?: (name: string) => string;
		lockedLabel?: string;
	}

	let {
		title = m.workspace_files(),
		leadingRows = [],
		entries,
		selectedFile,
		disabled = false,
		readOnlyMessage,
		onSelect,
		onCreateFile,
		onCreateFolder,
		onRename,
		onMove,
		onDelete,
		onUpload,
		onDownload,
		onRestore,
		multipleUploads = false,
		allowUploadOverwrite = false,
		validateName,
		emptyMessage = m.workspace_files_empty(),
		uploadDescription = m.workspace_upload_description(),
		rootDestinationLabel = m.workspace_root_destination(),
		rootPathMessage = m.workspace_root_path(),
		deleteConfirmMessage = (name) => m.workspace_delete_confirm({ name }),
		lockedLabel = m.workspace_file_read_only()
	}: Props = $props();

	let activeFolderPath = $state('');
	let dialogOpen = $state(false);
	let dialogMode = $state<DialogMode>('create_file');
	let dialogName = $state('');
	let dialogTargetPath = $state('');
	let dialogDestinationPath = $state('');
	let destinationOpenFolders = $state<Record<string, boolean>>({});
	let uploadFiles = $state<File[]>([]);
	let uploadInputKey = $state(0);
	let dialogSubmitting = $state(false);
	let dialogError = $state<string | null>(null);

	// Non-reactive @pierre/trees plumbing.
	let fileTree: PierreFileTree | undefined;
	let syncingSelection = false;
	let portaledMenu: HTMLElement | undefined;

	const TREE_ICON_SET = 'complete';

	function removePortaledMenu() {
		portaledMenu?.remove();
		portaledMenu = undefined;
	}

	const entryByPath = $derived.by(() => new Map(entries.map((entry) => [entry.relativePath, entry])));
	const selectedWorkspacePath = $derived(selectedFile.startsWith('file:') ? selectedFile.slice(5) : '');
	const selectedWorkspaceEntry = $derived(selectedWorkspacePath ? entryByPath.get(selectedWorkspacePath) : undefined);
	const selectedParentPath = $derived.by(() => {
		if (activeFolderPath && entryByPath.get(activeFolderPath)?.isDirectory) return activeFolderPath;
		return selectedWorkspaceEntry?.isDirectory
			? selectedWorkspaceEntry.relativePath
			: workspaceFileParentPath(selectedWorkspacePath);
	});
	const leadingFileRows = $derived(leadingRows.filter((row) => !row.action));
	const actionRows = $derived(leadingRows.filter((row) => row.action));
	const leadingByLabel = $derived(new Map(leadingFileRows.map((row) => [row.label, row])));
	const treePaths = $derived([
		...leadingFileRows.map((row) => row.label),
		...entries.map((entry) => (entry.isDirectory ? `${entry.relativePath}/` : entry.relativePath))
	]);
	const selectedTreePath = $derived.by(() => {
		if (selectedWorkspacePath) return selectedWorkspacePath;
		return leadingFileRows.find((row) => row.key === selectedFile)?.label ?? '';
	});
	const pendingStatus = $derived(
		entries
			.filter((entry) => entry.pending && !entry.isDirectory)
			.map((entry) => ({ path: entry.relativePath, status: 'modified' }) satisfies GitStatusEntry)
	);
	const canCreateFile = $derived(!!onCreateFile);
	const canCreateFolder = $derived(!!onCreateFolder);
	const canUpload = $derived(!!onUpload);
	const hasHeaderActions = $derived(canCreateFile || canCreateFolder || canUpload);
	const dialogTitle = $derived.by(() => {
		if (dialogMode === 'upload') return m.upload_file();
		if (dialogMode === 'move') return m.move();
		return dialogMode === 'create_folder' ? m.workspace_create_folder_title() : m.workspace_create_file_title();
	});
	const dialogActionLabel = $derived.by(() => {
		if (dialogMode === 'upload') return m.upload();
		return dialogMode === 'move' ? m.move() : m.common_create();
	});
	const allDestinationOptions = $derived.by(() =>
		dialogMode === 'move' && dialogTargetPath ? buildMoveDestinationOptions(dialogTargetPath) : buildFolderDestinationOptions()
	);
	const visibleDestinationOptions = $derived.by(() =>
		allDestinationOptions.filter((option) => option.relativePath === '' || isDestinationVisible(option.relativePath))
	);
	const hasValidDestination = $derived(allDestinationOptions.some((option) => !option.disabled));

	function isLockedPath(path: string): boolean {
		const leading = leadingByLabel.get(path);
		if (leading) return leading.locked === true;
		const entry = entryByPath.get(path);
		return !!entry && (entry.locked === true || entry.isSymlink === true);
	}

	function normalizeTreePath(path: string): string {
		return path.endsWith('/') ? path.slice(0, -1) : path;
	}

	function collectExpandedPaths(): string[] {
		if (!fileTree) return [];
		const count = fileTree.getVisibleCount();
		if (count === 0) return [];
		return fileTree
			.getVisibleRows(0, count - 1)
			.filter((row) => row.kind === 'directory' && row.isExpanded)
			.map((row) => row.path);
	}

	// Re-derive the tree from the entries source of truth (also used to revert
	// optimistic library-side renames/moves the parent rejected).
	function syncTree() {
		if (!fileTree) return;
		fileTree.resetPaths(treePaths, { initialExpandedPaths: collectExpandedPaths() });
		fileTree.setGitStatus(pendingStatus.length > 0 ? pendingStatus : undefined);
	}

	function scheduleTreeSync() {
		requestAnimationFrame(() => syncTree());
	}

	function expandTreePath(relativePath: string) {
		if (!relativePath) return;
		const item = fileTree?.getItem(relativePath);
		if (item?.isDirectory()) (item as FileTreeDirectoryHandle).expand();
	}

	function handleTreeSelectionChange(selectedPaths: readonly string[]) {
		if (syncingSelection) return;
		const path = selectedPaths[selectedPaths.length - 1];
		if (!path) return;
		const normalized = normalizeTreePath(path);

		const leading = leadingByLabel.get(normalized);
		if (leading) {
			activeFolderPath = '';
			if (selectedFile !== leading.key) {
				if (leading.onSelect) leading.onSelect();
				else onSelect(leading.key);
			}
			return;
		}

		const entry = entryByPath.get(normalized);
		if (!entry) return;
		if (entry.isDirectory) {
			activeFolderPath = entry.relativePath;
			return;
		}
		activeFolderPath = '';
		if (selectedFile !== `file:${entry.relativePath}`) {
			onSelect(`file:${entry.relativePath}`);
		}
	}

	function handleTreeRename(event: FileTreeRenameEvent) {
		const sourcePath = normalizeTreePath(event.sourcePath);
		const destinationPath = normalizeTreePath(event.destinationPath);
		const parentPath = workspaceFileParentPath(sourcePath);
		const name = normalizeDialogName(workspaceFileBasename(destinationPath), parentPath);
		if (!onRename || !name || (destinationPath !== sourcePath && entryByPath.has(destinationPath))) {
			scheduleTreeSync();
			return;
		}
		onRename(sourcePath, name);
	}

	function handleTreeDrop(event: FileTreeDropResult) {
		if (onMove) {
			const newParentPath = event.target.directoryPath ? normalizeTreePath(event.target.directoryPath) : '';
			for (const dragged of event.draggedPaths) {
				onMove(normalizeTreePath(dragged), newParentPath);
			}
		}
		scheduleTreeSync();
	}

	function renderTreeContextMenu(item: ContextMenuItem, context: ContextMenuOpenContext): HTMLElement | null {
		const entry = entryByPath.get(normalizeTreePath(item.path));
		if (!entry) return null;

		const locked = isLockedPath(entry.relativePath);
		const actions: Array<{ label: string; destructive?: boolean; run: () => void; closeOptions?: { restoreFocus?: boolean } }> =
			[];
		if (onDownload && !entry.isDirectory && !entry.pending) {
			actions.push({ label: m.templates_download(), run: () => onDownload?.(entry.relativePath) });
		}
		if (!locked && !disabled) {
			if (onRestore && !entry.isDirectory && !entry.pending) {
				actions.push({ label: m.workspace_restore(), run: () => onRestore?.(entry.relativePath) });
			}
			if (onRename) {
				actions.push({
					label: m.rename(),
					closeOptions: { restoreFocus: false },
					run: () => fileTree?.startRenaming(entry.relativePath)
				});
			}
			if (onMove) {
				actions.push({ label: m.move(), run: () => openMoveDialog(entry.relativePath) });
			}
			if (onDelete) {
				actions.push({ label: m.common_delete(), destructive: true, run: () => handleDelete(entry) });
			}
		}
		if (actions.length === 0) return null;

		// Portal the menu to the body: rendered inside the tree's shadow root it
		// is clipped by the panel's stacking context (paints behind the editor
		// pane). The dataset marker keeps clicks inside it from closing the menu.
		removePortaledMenu();
		const menu = document.createElement('div');
		menu.className = 'workspace-tree-menu';
		menu.dataset['fileTreeContextMenuRoot'] = 'true';
		menu.style.position = 'fixed';
		menu.style.zIndex = 'var(--arcane-z-popover, 60)';
		menu.style.left = `${context.anchorRect.left}px`;
		menu.style.top = `${context.anchorRect.bottom + 4}px`;
		const style = document.createElement('style');
		style.textContent = `
			.workspace-tree-menu {
				min-width: 10rem;
				padding: 0.25rem;
				border: 1px solid var(--border);
				border-radius: 0.5rem;
				background: var(--popover, var(--background));
				color: var(--popover-foreground, var(--foreground));
				box-shadow: 0 8px 24px rgba(0, 0, 0, 0.25);
			}
			.workspace-tree-menu button {
				display: block;
				width: 100%;
				padding: 0.3rem 0.6rem;
				text-align: left;
				font-size: 0.8rem;
				border: none;
				border-radius: 0.375rem;
				background: transparent;
				color: inherit;
				cursor: pointer;
			}
			.workspace-tree-menu button:hover {
				background: var(--accent);
			}
			.workspace-tree-menu button.destructive {
				color: var(--destructive);
			}
		`;
		menu.appendChild(style);
		for (const action of actions) {
			const button = document.createElement('button');
			button.type = 'button';
			button.textContent = action.label;
			if (action.destructive) button.classList.add('destructive');
			button.addEventListener('click', () => {
				context.close(action.closeOptions);
				action.run();
			});
			menu.appendChild(button);
		}
		document.body.appendChild(menu);
		portaledMenu = menu;
		requestAnimationFrame(() => {
			const rect = menu.getBoundingClientRect();
			if (rect.bottom > window.innerHeight) {
				menu.style.top = `${Math.max(4, context.anchorRect.top - rect.height - 4)}px`;
			}
			if (rect.right > window.innerWidth) {
				menu.style.left = `${Math.max(4, window.innerWidth - rect.width - 8)}px`;
			}
		});
		// The library keeps the menu session open against this in-shadow anchor.
		return document.createElement('div');
	}

	function treeAttachment(host: HTMLElement) {
		const context = { cancelled: false };

		void (async () => {
			const { FileTree } = await import('@pierre/trees');
			if (context.cancelled) return;

			const tree = new FileTree({
				paths: untrack(() => treePaths),
				icons: TREE_ICON_SET,
				// The host's inner flex wrapper shrink-wraps to the widest row, which
				// leaves row highlights short of the panel edge; stretch it instead.
				unsafeCSS: `
					:host { width: 100%; }
					[data-file-tree-virtualized-wrapper="true"],
					[data-file-tree-virtualized-root="true"] {
						flex: 1 1 auto;
						width: 100%;
						min-width: 0;
					}
				`,
				density: 'compact',
				initialSelectedPaths: untrack(() => (selectedTreePath ? [selectedTreePath] : [])),
				gitStatus: untrack(() => (pendingStatus.length > 0 ? pendingStatus : undefined)),
				onSelectionChange: handleTreeSelectionChange,
				renaming: {
					canRename: (item) => !disabled && !!onRename && !isLockedPath(normalizeTreePath(item.path)),
					onRename: handleTreeRename
				},
				dragAndDrop: {
					canDrag: (paths) => !disabled && !!onMove && paths.every((path) => !isLockedPath(normalizeTreePath(path))),
					onDropComplete: handleTreeDrop,
					onDropError: () => scheduleTreeSync()
				},
				composition: {
					contextMenu: {
						enabled: true,
						triggerMode: 'both',
						buttonVisibility: 'when-needed',
						render: renderTreeContextMenu,
						onClose: () => removePortaledMenu()
					}
				},
				renderRowDecoration: ({ item }) =>
					isLockedPath(normalizeTreePath(item.path)) ? { icon: 'file-tree-icon-lock', title: lockedLabel } : null
			});
			fileTree = tree;
			tree.render({ fileTreeContainer: host });
		})();

		return () => {
			context.cancelled = true;
			removePortaledMenu();
			fileTree?.cleanUp();
			fileTree = undefined;
		};
	}

	// Keep the tree in sync with the entries source of truth.
	$effect(() => {
		void treePaths;
		void pendingStatus;
		untrack(() => syncTree());
	});

	// Reflect external selection (tabs, leading rows) into the tree.
	$effect(() => {
		const path = selectedTreePath;
		untrack(() => {
			if (!fileTree) return;
			syncingSelection = true;
			try {
				for (const selected of fileTree.getSelectedPaths()) {
					if (normalizeTreePath(selected) !== path) fileTree.getItem(selected)?.deselect();
				}
				if (path) fileTree.getItem(path)?.select();
			} finally {
				syncingSelection = false;
			}
		});
	});

	function openCreateDialog(mode: Extract<DialogMode, 'create_file' | 'create_folder'>, parentPath = selectedParentPath) {
		if (disabled) return;
		dialogMode = mode;
		dialogName = '';
		dialogTargetPath = '';
		dialogDestinationPath = parentPath;
		destinationOpenFolders = parentPath ? openAncestorDestinationFolders(parentPath) : {};
		uploadFiles = [];
		dialogSubmitting = false;
		dialogError = null;
		dialogOpen = true;
	}

	function compareWorkspaceFolderPaths(a: string, b: string): number {
		const aSegments = a.split('/');
		const bSegments = b.split('/');
		const length = Math.min(aSegments.length, bSegments.length);

		for (let index = 0; index < length; index += 1) {
			const aSegment = aSegments[index] ?? '';
			const bSegment = bSegments[index] ?? '';
			const baseComparison = aSegment.localeCompare(bSegment, undefined, { sensitivity: 'base' });
			if (baseComparison !== 0) return baseComparison;

			const exactComparison = aSegment.localeCompare(bSegment);
			if (exactComparison !== 0) return exactComparison;
		}

		return aSegments.length - bSegments.length;
	}

	function destinationDepth(relativePath: string): number {
		return relativePath ? relativePath.split('/').length - 1 : 0;
	}

	function buildDestinationChildCounts(): Map<string, number> {
		const childCounts = new Map<string, number>();
		for (const entry of entries) {
			if (!entry.isDirectory) continue;
			const parentPath = workspaceFileParentPath(entry.relativePath);
			childCounts.set(parentPath, (childCounts.get(parentPath) ?? 0) + 1);
		}
		return childCounts;
	}

	function buildFolderDestinationOptions(): FolderDestinationOption[] {
		const childCounts = buildDestinationChildCounts();
		return [
			{
				relativePath: '',
				label: rootDestinationLabel,
				depth: 0,
				hasChildren: (childCounts.get('') ?? 0) > 0,
				disabled: false
			},
			...entries
				.filter((candidate) => candidate.isDirectory)
				.sort((a, b) => compareWorkspaceFolderPaths(a.relativePath, b.relativePath))
				.map((candidate) => ({
					relativePath: candidate.relativePath,
					label: workspaceFileBasename(candidate.relativePath),
					depth: destinationDepth(candidate.relativePath),
					hasChildren: (childCounts.get(candidate.relativePath) ?? 0) > 0,
					disabled: false
				}))
		];
	}

	function buildMoveDestinationOptions(relativePath: string): FolderDestinationOption[] {
		const entry = entryByPath.get(relativePath);
		if (!entry) return [];

		const basename = workspaceFileBasename(relativePath);
		const currentParentPath = workspaceFileParentPath(relativePath);
		return buildFolderDestinationOptions().map((candidate) => {
			const targetPath = joinWorkspaceFilePath(candidate.relativePath, basename);
			let reason: string | undefined;
			if (candidate.relativePath === currentParentPath) {
				reason = m.workspace_move_current_location();
			} else if (entry.isDirectory && candidate.relativePath && workspaceFilePathMatches(candidate.relativePath, relativePath)) {
				reason = m.workspace_move_descendant_blocked();
			} else if (entryByPath.has(targetPath)) {
				reason = m.workspace_move_duplicate_destination();
			}

			return {
				...candidate,
				disabled: !!reason,
				reason
			};
		});
	}

	function openAncestorDestinationFolders(relativePath: string): Record<string, boolean> {
		const folders: Record<string, boolean> = {};
		let parentPath = workspaceFileParentPath(relativePath);
		while (parentPath) {
			folders[parentPath] = true;
			parentPath = workspaceFileParentPath(parentPath);
		}
		return folders;
	}

	function isDestinationVisible(relativePath: string): boolean {
		let parentPath = workspaceFileParentPath(relativePath);
		while (parentPath) {
			if (destinationOpenFolders[parentPath] !== true) return false;
			parentPath = workspaceFileParentPath(parentPath);
		}
		return true;
	}

	function toggleDestinationFolder(relativePath: string) {
		destinationOpenFolders = {
			...destinationOpenFolders,
			[relativePath]: destinationOpenFolders[relativePath] !== true
		};
	}

	function openMoveDialog(relativePath: string) {
		if (disabled) return;
		const destinations = buildMoveDestinationOptions(relativePath);
		const selectedDestinationPath = destinations.find((destination) => !destination.disabled)?.relativePath ?? '';
		dialogMode = 'move';
		dialogName = '';
		dialogTargetPath = relativePath;
		dialogDestinationPath = selectedDestinationPath;
		destinationOpenFolders = selectedDestinationPath ? openAncestorDestinationFolders(selectedDestinationPath) : {};
		uploadFiles = [];
		dialogSubmitting = false;
		dialogError = null;
		dialogOpen = true;
	}

	function openUploadDialog(parentPath = selectedParentPath) {
		if (disabled) return;
		dialogMode = 'upload';
		dialogName = '';
		dialogTargetPath = '';
		dialogDestinationPath = parentPath;
		destinationOpenFolders = parentPath ? openAncestorDestinationFolders(parentPath) : {};
		uploadFiles = [];
		uploadInputKey += 1;
		dialogSubmitting = false;
		dialogError = null;
		dialogOpen = true;
	}

	function handleUploadFileChange(event: Event) {
		const input = event.currentTarget as HTMLInputElement;
		uploadFiles = input.files ? [...input.files] : [];
		if (uploadFiles.length === 1) {
			dialogName = uploadFiles[0]?.name ?? '';
		}
		dialogError = null;
	}

	function normalizeDialogName(name: string, parentPath: string): string | null {
		return validateName ? validateName(name, parentPath) : validateWorkspaceFileName(name);
	}

	async function handleDialogSubmit() {
		if (dialogMode === 'move') {
			const destination = allDestinationOptions.find((option) => option.relativePath === dialogDestinationPath);
			if (!destination || destination.disabled) {
				dialogError = destination?.reason ?? m.workspace_file_invalid_move_destination();
				return;
			}

			onMove?.(dialogTargetPath, dialogDestinationPath);
			expandTreePath(dialogDestinationPath);
			dialogOpen = false;
			return;
		}

		if (dialogMode === 'upload') {
			if (uploadFiles.length === 0) {
				dialogError = m.workspace_upload_file_required();
				return;
			}
			const normalizedNames = multipleUploads
				? uploadFiles.map((file) => normalizeDialogName(file.name, dialogDestinationPath))
				: [normalizeDialogName(dialogName, dialogDestinationPath)];
			if (normalizedNames.some((name) => !name)) {
				dialogError = m.workspace_file_invalid_name();
				return;
			}
			if (
				!allowUploadOverwrite &&
				normalizedNames.some((name) => name && entryByPath.has(joinWorkspaceFilePath(dialogDestinationPath, name)))
			) {
				dialogError = m.workspace_file_duplicate_name();
				return;
			}

			dialogSubmitting = true;
			try {
				const selectedFiles = multipleUploads
					? uploadFiles
					: [
							new File([uploadFiles[0]!], normalizedNames[0]!, {
								type: uploadFiles[0]!.type,
								lastModified: uploadFiles[0]!.lastModified
							})
						];
				const error = await onUpload?.(dialogDestinationPath, selectedFiles);
				if (typeof error === 'string' && error) {
					dialogError = error;
					return;
				}
				expandTreePath(dialogDestinationPath);
				dialogOpen = false;
			} finally {
				dialogSubmitting = false;
			}
			return;
		}

		const name = normalizeDialogName(dialogName, dialogDestinationPath);
		if (!name) {
			dialogError = m.workspace_file_invalid_name();
			return;
		}

		const targetPath = joinWorkspaceFilePath(dialogDestinationPath, name);
		if (entryByPath.has(targetPath)) {
			dialogError = m.workspace_file_duplicate_name();
			return;
		}

		if (dialogMode === 'create_folder') {
			onCreateFolder?.(dialogDestinationPath, name);
		} else {
			onCreateFile?.(dialogDestinationPath, name);
		}
		expandTreePath(dialogDestinationPath);

		dialogOpen = false;
	}

	function handleDelete(entry: WorkspaceFileEntry) {
		if (disabled) return;
		openConfirmDialog({
			title: m.delete_name({ name: entry.relativePath }),
			message: deleteConfirmMessage(entry.relativePath),
			confirm: {
				label: m.common_delete(),
				destructive: true,
				action: () => onDelete?.(entry.relativePath)
			}
		});
	}
</script>

<div class="flex min-h-0 flex-1 flex-col">
	<div class="flex h-9 shrink-0 items-center border-b border-border px-2">
		<span class="text-[11px] font-semibold tracking-wider text-muted-foreground uppercase">{title}</span>
		{#if hasHeaderActions}
			<div class="ml-auto flex items-center gap-0.5">
				{#if canCreateFile}
					<Tooltip.Root>
						<Tooltip.Trigger>
							<ArcaneButton
								action="create"
								size="icon"
								tone="ghost"
								class="size-6"
								icon={CreateFileIcon}
								showLabel={false}
								{disabled}
								customLabel={m.workspace_new_file()}
								onclick={() => openCreateDialog('create_file')}
							/>
						</Tooltip.Trigger>
						<Tooltip.Content>{m.workspace_new_file()}</Tooltip.Content>
					</Tooltip.Root>
				{/if}
				{#if canCreateFolder}
					<Tooltip.Root>
						<Tooltip.Trigger>
							<ArcaneButton
								action="create"
								size="icon"
								tone="ghost"
								class="size-6"
								icon={CreateFolderIcon}
								showLabel={false}
								{disabled}
								customLabel={m.new_folder()}
								onclick={() => openCreateDialog('create_folder')}
							/>
						</Tooltip.Trigger>
						<Tooltip.Content>{m.new_folder()}</Tooltip.Content>
					</Tooltip.Root>
				{/if}
				{#if canUpload}
					<Tooltip.Root>
						<Tooltip.Trigger>
							<ArcaneButton
								action="base"
								size="icon"
								tone="ghost"
								class="size-6"
								icon={UploadIcon}
								showLabel={false}
								{disabled}
								customLabel={m.upload_file()}
								onclick={() => openUploadDialog()}
							/>
						</Tooltip.Trigger>
						<Tooltip.Content>{m.upload_file()}</Tooltip.Content>
					</Tooltip.Root>
				{/if}
			</div>
		{/if}
	</div>

	{#if readOnlyMessage}
		<div class="border-b border-border px-3 py-2 text-xs text-muted-foreground">{readOnlyMessage}</div>
	{/if}

	{#if actionRows.length > 0}
		<div class="shrink-0 px-2 pt-1.5">
			{#each actionRows as leadingRow (leadingRow.key)}
				<button
					type="button"
					class={cn(
						'flex w-full items-center gap-1.5 rounded-md px-2 py-1 text-left text-[13px] text-muted-foreground hover:bg-accent hover:text-foreground',
						selectedFile === leadingRow.key && 'bg-accent'
					)}
					onclick={() => (leadingRow.onSelect ? leadingRow.onSelect() : onSelect(leadingRow.key))}
				>
					<CreateFileIcon class="size-4 shrink-0" />
					<span class="min-w-0 flex-1 truncate">{leadingRow.label}</span>
				</button>
			{/each}
		</div>
	{/if}

	{#if treePaths.length === 0}
		<div class="px-7 py-3 text-xs text-muted-foreground">{emptyMessage}</div>
	{:else}
		<div
			class="min-h-0 flex-1 px-1 pt-1 pb-1"
			style="--trees-padding-inline-override: 4px; --trees-bg-override: var(--background); --trees-theme-list-active-selection-bg: var(--accent); --trees-theme-list-hover-bg: color-mix(in oklab, var(--accent) 55%, transparent); --trees-theme-focus-ring: var(--primary)"
			{@attach treeAttachment}
		></div>
	{/if}
</div>

<Dialog.Root bind:open={dialogOpen}>
	<Dialog.Content class="max-h-[calc(100vh-2rem)] max-w-2xl overflow-hidden">
		<form
			class="flex max-h-[calc(100vh-5rem)] min-h-0 flex-col gap-4"
			onsubmit={(event) => {
				event.preventDefault();
				void handleDialogSubmit();
			}}
		>
			<Dialog.Header>
				<Dialog.Title>{dialogTitle}</Dialog.Title>
				<Dialog.Description>
					{#if dialogMode === 'move'}
						{m.workspace_file_move_description({ name: dialogTargetPath })}
					{:else if dialogMode === 'upload'}
						{uploadDescription}
					{:else}
						{dialogDestinationPath ? m.workspace_file_parent_path({ path: dialogDestinationPath }) : rootPathMessage}
					{/if}
				</Dialog.Description>
			</Dialog.Header>

			{#if dialogMode === 'upload'}
				<div class="space-y-2">
					<Label for="workspace-file-upload">{m.workspace_upload_file_label()}</Label>
					{#key uploadInputKey}
						<Input
							id="workspace-file-upload"
							type="file"
							multiple={multipleUploads}
							onchange={handleUploadFileChange}
							aria-invalid={!!dialogError}
						/>
					{/key}
				</div>
			{/if}

			{#if dialogMode !== 'move' && (dialogMode !== 'upload' || !multipleUploads)}
				<div class="space-y-2">
					<Label for="workspace-file-name">{m.common_name()}</Label>
					<Input
						id="workspace-file-name"
						bind:value={dialogName}
						placeholder={dialogMode === 'create_folder'
							? m.workspace_folder_name_placeholder()
							: m.workspace_file_name_placeholder()}
						aria-invalid={!!dialogError}
					/>
				</div>
			{/if}

			<div class="min-h-0 space-y-2">
				<Label>{m.workspace_file_move_destination_label()}</Label>
				<div class="max-h-[56vh] min-h-80 space-y-1 overflow-auto rounded-md border p-1">
					{#each visibleDestinationOptions as option (option.relativePath)}
						<div
							class={cn(
								'flex w-full items-center gap-2 rounded px-2 py-1.5 text-left text-sm',
								dialogDestinationPath === option.relativePath && !option.disabled && 'bg-accent',
								option.disabled ? 'opacity-45' : 'hover:bg-accent'
							)}
							style={`padding-left: ${0.5 + option.depth * 1.25}rem`}
						>
							{#if option.relativePath && option.hasChildren}
								<button
									type="button"
									class="inline-flex size-4 shrink-0 items-center justify-center rounded hover:bg-muted"
									aria-label={destinationOpenFolders[option.relativePath]
										? m.workspace_file_collapse_folder({ name: option.label })
										: m.workspace_file_expand_folder({ name: option.label })}
									onclick={() => toggleDestinationFolder(option.relativePath)}
								>
									{#if destinationOpenFolders[option.relativePath] === true}
										<ArrowDownIcon class="size-4" />
									{:else}
										<ArrowRightIcon class="size-4" />
									{/if}
								</button>
							{:else}
								<span class="inline-flex size-4 shrink-0 items-center justify-center"></span>
							{/if}
							<button
								type="button"
								class={cn('flex min-w-0 flex-1 items-center gap-2 text-left', option.disabled && 'cursor-not-allowed')}
								disabled={option.disabled}
								title={option.relativePath || option.label}
								onclick={() => {
									dialogDestinationPath = option.relativePath;
									dialogError = null;
								}}
							>
								<FolderOpenIcon class="size-4 shrink-0 text-amber-500" />
								<span class="min-w-0 flex-1 truncate">{option.label}</span>
							</button>
							{#if option.reason}
								<span class="shrink-0 text-xs text-muted-foreground">{option.reason}</span>
							{/if}
						</div>
					{/each}
				</div>
			</div>

			{#if dialogError}
				<p class="text-sm text-destructive">{dialogError}</p>
			{/if}

			<Dialog.Footer>
				<ArcaneButton action="cancel" onclick={() => (dialogOpen = false)} />
				<ArcaneButton
					action="confirm"
					type="submit"
					customLabel={dialogActionLabel}
					loading={dialogSubmitting}
					disabled={dialogSubmitting ||
						(dialogMode === 'move' && !hasValidDestination) ||
						(dialogMode === 'upload' && uploadFiles.length === 0)}
				/>
			</Dialog.Footer>
		</form>
	</Dialog.Content>
</Dialog.Root>
