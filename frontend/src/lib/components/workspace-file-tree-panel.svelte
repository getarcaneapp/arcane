<script lang="ts">
	import { ArcaneButton } from '#lib/components/arcane-button/index.js';
	import { openConfirmDialog } from '#lib/components/confirm-dialog';
	import * as Dialog from '#lib/components/ui/dialog';
	import { Input } from '#lib/components/ui/input';
	import { Label } from '#lib/components/ui/label';
	import * as Tooltip from '#lib/components/ui/tooltip/index.js';
	import * as TreeView from '#lib/components/ui/tree-view/index.js';
	import {
		ArrowDownIcon,
		ArrowRightIcon,
		CreateFileIcon,
		CreateFolderIcon,
		DownloadIcon,
		EditIcon,
		FileTextIcon,
		FolderMoveIcon,
		FolderOpenIcon,
		LockIcon,
		RefreshIcon,
		TrashIcon,
		UploadIcon
	} from '#lib/icons';
	import { m } from '#lib/paraglide/messages';
	import { cn } from '#lib/utils';
	import {
		compareWorkspaceFileEntries,
		joinWorkspaceFilePath,
		workspaceFileBasename,
		workspaceFileParentPath,
		workspaceFilePathMatches,
		validateWorkspaceFileName,
		type WorkspaceFileEntry
	} from '#lib/utils/workspace-files';

	type DialogMode = 'create_file' | 'create_folder' | 'rename' | 'move' | 'upload';
	type TreeRow = WorkspaceFileEntry & {
		depth: number;
		hasChildren: boolean;
	};
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

	let openFolders = $state<Record<string, boolean>>({});
	let activeFolderPath = $state('');
	let dialogOpen = $state(false);
	let dialogMode = $state<DialogMode>('create_file');
	let dialogName = $state('');
	let dialogParentPath = $state('');
	let dialogTargetPath = $state('');
	let dialogDestinationPath = $state('');
	let destinationOpenFolders = $state<Record<string, boolean>>({});
	let uploadFiles = $state<File[]>([]);
	let uploadInputKey = $state(0);
	let dialogSubmitting = $state(false);
	let dialogError = $state<string | null>(null);

	const entryByPath = $derived.by(() => new Map(entries.map((entry) => [entry.relativePath, entry])));
	const selectedWorkspacePath = $derived(selectedFile.startsWith('file:') ? selectedFile.slice(5) : '');
	const selectedWorkspaceEntry = $derived(selectedWorkspacePath ? entryByPath.get(selectedWorkspacePath) : undefined);
	const selectedParentPath = $derived.by(() => {
		if (activeFolderPath && entryByPath.get(activeFolderPath)?.isDirectory) return activeFolderPath;
		return selectedWorkspaceEntry?.isDirectory
			? selectedWorkspaceEntry.relativePath
			: workspaceFileParentPath(selectedWorkspacePath);
	});
	const rows = $derived.by(() => flattenRows(entries, openFolders));
	const hasDirectories = $derived(entries.some((entry) => entry.isDirectory));
	const canCreateFile = $derived(!!onCreateFile);
	const canCreateFolder = $derived(!!onCreateFolder);
	const canUpload = $derived(!!onUpload);
	const hasHeaderActions = $derived(canCreateFile || canCreateFolder || canUpload);
	const dialogTitle = $derived.by(() => {
		if (dialogMode === 'upload') return m.upload_file();
		if (dialogMode === 'move') return m.move();
		if (dialogMode === 'rename') return m.rename();
		return dialogMode === 'create_folder' ? m.workspace_create_folder_title() : m.workspace_create_file_title();
	});
	const dialogActionLabel = $derived.by(() => {
		if (dialogMode === 'upload') return m.upload();
		if (dialogMode === 'move') return m.move();
		return dialogMode === 'rename' ? m.rename() : m.common_create();
	});
	const hasDestinationPicker = $derived(
		dialogMode === 'create_file' || dialogMode === 'create_folder' || dialogMode === 'upload' || dialogMode === 'move'
	);
	const allDestinationOptions = $derived.by(() =>
		hasDestinationPicker
			? dialogMode === 'move' && dialogTargetPath
				? buildMoveDestinationOptions(dialogTargetPath)
				: buildFolderDestinationOptions()
			: []
	);
	const visibleDestinationOptions = $derived.by(() =>
		allDestinationOptions.filter((option) => option.relativePath === '' || isDestinationVisible(option.relativePath))
	);
	const hasValidDestination = $derived(allDestinationOptions.some((option) => !option.disabled));

	function toggleFolder(relativePath: string) {
		activeFolderPath = relativePath;
		openFolders = {
			...openFolders,
			[relativePath]: openFolders[relativePath] !== true
		};
	}

	function flattenRows(files: WorkspaceFileEntry[], folderStates: Record<string, boolean>): TreeRow[] {
		const byParent = new Map<string, WorkspaceFileEntry[]>();
		for (const entry of files) {
			const parentPath = workspaceFileParentPath(entry.relativePath);
			const siblings = byParent.get(parentPath) ?? [];
			siblings.push(entry);
			byParent.set(parentPath, siblings);
		}
		for (const siblings of byParent.values()) {
			siblings.sort(compareWorkspaceFileEntries);
		}

		const result: TreeRow[] = [];
		const appendRows = (parentPath: string, depth: number) => {
			for (const entry of byParent.get(parentPath) ?? []) {
				const hasChildren = (byParent.get(entry.relativePath) ?? []).length > 0;
				result.push({ ...entry, depth, hasChildren });
				if (entry.isDirectory && folderStates[entry.relativePath] === true) {
					appendRows(entry.relativePath, depth + 1);
				}
			}
		};

		appendRows('', 0);
		return result;
	}

	function openCreateDialog(mode: Extract<DialogMode, 'create_file' | 'create_folder'>, parentPath = selectedParentPath) {
		if (disabled) return;
		dialogMode = mode;
		dialogName = '';
		dialogParentPath = '';
		dialogTargetPath = '';
		dialogDestinationPath = parentPath;
		destinationOpenFolders = parentPath ? openAncestorDestinationFolders(parentPath) : {};
		uploadFiles = [];
		dialogSubmitting = false;
		dialogError = null;
		dialogOpen = true;
	}

	function openRenameDialog(relativePath: string) {
		if (disabled) return;
		dialogMode = 'rename';
		dialogName = workspaceFileBasename(relativePath);
		dialogParentPath = workspaceFileParentPath(relativePath);
		dialogTargetPath = relativePath;
		dialogDestinationPath = '';
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
		dialogParentPath = '';
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
		dialogParentPath = '';
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
			if (dialogDestinationPath) {
				openFolders = {
					...openFolders,
					[dialogDestinationPath]: true
				};
			}
			dialogOpen = false;
			return;
		}

		if (dialogMode === 'rename') {
			const name = normalizeDialogName(dialogName, dialogParentPath);
			if (!name) {
				dialogError = m.workspace_file_invalid_name();
				return;
			}

			const targetPath = joinWorkspaceFilePath(dialogParentPath, name);
			if (targetPath !== dialogTargetPath && entryByPath.has(targetPath)) {
				dialogError = m.workspace_file_duplicate_name();
				return;
			}

			onRename?.(dialogTargetPath, name);
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
				if (dialogDestinationPath) {
					openFolders = { ...openFolders, [dialogDestinationPath]: true };
				}
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
			openFolders = {
				...openFolders,
				[dialogDestinationPath]: true
			};
		} else {
			onCreateFile?.(dialogDestinationPath, name);
			openFolders = {
				...openFolders,
				[dialogDestinationPath]: true
			};
		}

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

	<div class="min-h-0 flex-1 overflow-auto">
		<TreeView.Root class="min-w-max p-2 whitespace-nowrap">
			{#each leadingRows as leadingRow (leadingRow.key)}
				<button
					type="button"
					class={cn(
						'flex w-full items-center gap-1.5 rounded-md px-2 py-1 text-left text-[13px] hover:bg-accent',
						selectedFile === leadingRow.key && 'bg-accent',
						leadingRow.action && 'text-muted-foreground hover:text-foreground'
					)}
					onclick={() => (leadingRow.onSelect ? leadingRow.onSelect() : onSelect(leadingRow.key))}
				>
					{#if hasDirectories}
						<span class="inline-flex size-4 shrink-0 items-center justify-center"></span>
					{/if}
					{#if leadingRow.action}
						<CreateFileIcon class="size-4 shrink-0" />
					{:else}
						<FileTextIcon class={cn('size-4 shrink-0', leadingRow.iconClass ?? 'text-muted-foreground')} />
					{/if}
					<span class="min-w-0 flex-1 truncate">{leadingRow.label}</span>
					{#if leadingRow.locked}
						<span class="inline-flex size-6 shrink-0 items-center justify-center">
							<LockIcon class="size-3.5 shrink-0 text-muted-foreground" aria-label={lockedLabel} />
						</span>
					{/if}
				</button>
			{/each}

			{#if rows.length === 0}
				<div class="px-7 py-3 text-xs text-muted-foreground">{emptyMessage}</div>
			{:else}
				{#each rows as row (row.relativePath)}
					<div
						class={cn(
							'group flex w-full items-center gap-1.5 rounded-md px-2 py-0.5 text-[13px] hover:bg-accent',
							selectedFile === `file:${row.relativePath}` && 'bg-accent'
						)}
						style={`padding-left: ${0.5 + row.depth * 1}rem`}
					>
						{#if row.isDirectory}
							<button
								type="button"
								class="inline-flex size-4 shrink-0 items-center justify-center rounded hover:bg-muted"
								aria-label={openFolders[row.relativePath]
									? m.workspace_file_collapse_folder({ name: row.name })
									: m.workspace_file_expand_folder({ name: row.name })}
								onclick={() => toggleFolder(row.relativePath)}
							>
								{#if openFolders[row.relativePath] === true}
									<ArrowDownIcon class="size-3.5" />
								{:else}
									<ArrowRightIcon class="size-3.5" />
								{/if}
							</button>
						{:else if hasDirectories}
							<span class="inline-flex size-4 shrink-0 items-center justify-center"></span>
						{/if}

						<button
							type="button"
							class="flex min-w-0 flex-1 items-center gap-1.5 py-1 text-left"
							onclick={() => (row.isDirectory ? toggleFolder(row.relativePath) : onSelect(`file:${row.relativePath}`))}
						>
							{#if row.isDirectory}
								<FolderOpenIcon class="size-4 shrink-0 text-amber-500" />
							{:else}
								<FileTextIcon class="size-4 shrink-0 text-muted-foreground" />
							{/if}
							<span class="min-w-0 truncate">{row.name}</span>
							{#if row.pending}
								<span
									class="size-1.5 shrink-0 rounded-full bg-primary"
									role="img"
									aria-label={m.common_unsaved_changes()}
									title={m.common_unsaved_changes()}
								></span>
							{/if}
						</button>

						{#if row.locked || row.isSymlink || onRename || onMove || onDelete || onDownload || onRestore}
							<div class="flex shrink-0 items-center gap-0.5">
								{#if onDownload && !row.isDirectory && !row.pending}
									<Tooltip.Root>
										<Tooltip.Trigger>
											<button
												type="button"
												class="inline-flex size-6 items-center justify-center rounded text-foreground hover:bg-foreground/10"
												aria-label={m.templates_download()}
												onclick={() => onDownload?.(row.relativePath)}
											>
												<DownloadIcon class="size-3.5" />
											</button>
										</Tooltip.Trigger>
										<Tooltip.Content>{m.templates_download()}</Tooltip.Content>
									</Tooltip.Root>
								{/if}
								{#if row.locked || row.isSymlink}
									<LockIcon class="mx-1 size-3.5 shrink-0 text-muted-foreground" aria-label={lockedLabel} />
								{:else}
									{#if onRestore && !row.isDirectory && !row.pending}
										<Tooltip.Root>
											<Tooltip.Trigger>
												<button
													type="button"
													class="inline-flex size-6 items-center justify-center rounded text-foreground hover:bg-foreground/10"
													aria-label={m.workspace_restore()}
													onclick={() => onRestore?.(row.relativePath)}
												>
													<RefreshIcon class="size-3.5" />
												</button>
											</Tooltip.Trigger>
											<Tooltip.Content>{m.workspace_restore()}</Tooltip.Content>
										</Tooltip.Root>
									{/if}
									{#if onRename}
										<Tooltip.Root>
											<Tooltip.Trigger>
												<button
													type="button"
													class="inline-flex size-6 items-center justify-center rounded text-foreground hover:bg-foreground/10"
													aria-label={m.workspace_file_rename_label({ name: row.relativePath })}
													{disabled}
													onclick={() => openRenameDialog(row.relativePath)}
												>
													<EditIcon class="size-3.5" />
												</button>
											</Tooltip.Trigger>
											<Tooltip.Content>{m.rename()}</Tooltip.Content>
										</Tooltip.Root>
									{/if}
									{#if onMove}
										<Tooltip.Root>
											<Tooltip.Trigger>
												<button
													type="button"
													class="inline-flex size-6 items-center justify-center rounded text-foreground hover:bg-foreground/10"
													aria-label={m.workspace_file_move_label({ name: row.relativePath })}
													{disabled}
													onclick={() => openMoveDialog(row.relativePath)}
												>
													<FolderMoveIcon class="size-3.5" />
												</button>
											</Tooltip.Trigger>
											<Tooltip.Content>{m.move()}</Tooltip.Content>
										</Tooltip.Root>
									{/if}
									{#if onDelete}
										<Tooltip.Root>
											<Tooltip.Trigger>
												<button
													type="button"
													class="inline-flex size-6 items-center justify-center rounded text-destructive hover:bg-destructive/10"
													aria-label={m.delete_name({ name: row.relativePath })}
													{disabled}
													onclick={() => handleDelete(row)}
												>
													<TrashIcon class="size-3.5" />
												</button>
											</Tooltip.Trigger>
											<Tooltip.Content>{m.common_delete()}</Tooltip.Content>
										</Tooltip.Root>
									{/if}
								{/if}
							</div>
						{/if}
					</div>
				{/each}
			{/if}
		</TreeView.Root>
	</div>
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
					{:else if dialogMode === 'create_file' || dialogMode === 'create_folder'}
						{dialogDestinationPath ? m.workspace_file_parent_path({ path: dialogDestinationPath }) : rootPathMessage}
					{:else}
						{dialogParentPath ? m.workspace_file_parent_path({ path: dialogParentPath }) : rootPathMessage}
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

			{#if hasDestinationPicker}
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
			{/if}

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
