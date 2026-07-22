<script lang="ts">
	import { ArcaneButton } from '$lib/components/arcane-button/index.js';
	import { openConfirmDialog } from '$lib/components/confirm-dialog';
	import * as Dialog from '$lib/components/ui/dialog';
	import { Input } from '$lib/components/ui/input';
	import { Label } from '$lib/components/ui/label';
	import * as Tooltip from '$lib/components/ui/tooltip/index.js';
	import * as TreeView from '$lib/components/ui/tree-view/index.js';
	import {
		ArrowDownIcon,
		ArrowRightIcon,
		CreateFileIcon,
		CreateFolderIcon,
		EditIcon,
		FileTextIcon,
		FolderMoveIcon,
		FolderOpenIcon,
		LockIcon,
		TrashIcon,
		UploadIcon
	} from '$lib/icons';
	import { m } from '$lib/paraglide/messages';
	import { cn } from '$lib/utils';
	import {
		compareProjectFileEntries,
		joinProjectFilePath,
		projectFileBasename,
		projectFileParentPath,
		projectFilePathMatches,
		validateProjectFileName,
		type ManagedProjectFileEntry
	} from './project-file-tree-utils';

	const MAX_MANAGED_PROJECT_FILE_BYTES = 1024 * 1024;

	type DialogMode = 'create_file' | 'create_folder' | 'rename' | 'move' | 'upload';
	type TreeRow = ManagedProjectFileEntry & {
		depth: number;
		hasChildren: boolean;
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
		composeFileName: string;
		// The override row is pinned only by the project editor. showOverride renders
		// the existing-override row (labeled overrideFileName); onAddOverride renders
		// an "add override" affordance for a project that has none yet. Other
		// consumers (new project, swarm stacks) omit all three.
		overrideFileName?: string;
		showOverride?: boolean;
		onAddOverride?: () => void;
		entries: ManagedProjectFileEntry[];
		selectedFile: string;
		disabled?: boolean;
		readOnlyMessage?: string;
		onSelect: (key: string) => void;
		onCreateFile?: (parentPath: string, name: string, content?: string) => void;
		onCreateFolder?: (parentPath: string, name: string) => void;
		onRename?: (relativePath: string, newName: string) => void;
		onMove?: (relativePath: string, newParentPath: string) => void;
		onDelete?: (relativePath: string) => void;
	}

	let {
		composeFileName,
		overrideFileName,
		showOverride = false,
		onAddOverride,
		entries,
		selectedFile,
		disabled = false,
		readOnlyMessage,
		onSelect,
		onCreateFile,
		onCreateFolder,
		onRename,
		onMove,
		onDelete
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
	let uploadFile = $state<File | null>(null);
	let uploadInputKey = $state(0);
	let dialogSubmitting = $state(false);
	let dialogError = $state<string | null>(null);

	const entryByPath = $derived.by(() => new Map(entries.map((entry) => [entry.relativePath, entry])));
	const selectedManagedPath = $derived(selectedFile.startsWith('file:') ? selectedFile.slice(5) : '');
	const selectedManagedEntry = $derived(selectedManagedPath ? entryByPath.get(selectedManagedPath) : undefined);
	const selectedParentPath = $derived.by(() => {
		if (activeFolderPath && entryByPath.get(activeFolderPath)?.isDirectory) return activeFolderPath;
		return selectedManagedEntry?.isDirectory ? selectedManagedEntry.relativePath : projectFileParentPath(selectedManagedPath);
	});
	const rows = $derived.by(() => flattenRows(entries, openFolders));
	const hasDirectories = $derived(entries.some((entry) => entry.isDirectory));
	const canManageFiles = $derived(!!onCreateFile && !!onCreateFolder && !!onRename && !!onMove && !!onDelete);
	const dialogTitle = $derived.by(() => {
		if (dialogMode === 'upload') return m.upload_file();
		if (dialogMode === 'move') return m.move();
		if (dialogMode === 'rename') return m.rename();
		return dialogMode === 'create_folder' ? m.project_file_create_folder_title() : m.project_file_create_file_title();
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

	function flattenRows(files: ManagedProjectFileEntry[], folderStates: Record<string, boolean>): TreeRow[] {
		const byParent = new Map<string, ManagedProjectFileEntry[]>();
		for (const entry of files) {
			const parentPath = projectFileParentPath(entry.relativePath);
			const siblings = byParent.get(parentPath) ?? [];
			siblings.push(entry);
			byParent.set(parentPath, siblings);
		}
		for (const siblings of byParent.values()) {
			siblings.sort(compareProjectFileEntries);
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
		uploadFile = null;
		dialogSubmitting = false;
		dialogError = null;
		dialogOpen = true;
	}

	function openRenameDialog(relativePath: string) {
		if (disabled) return;
		dialogMode = 'rename';
		dialogName = projectFileBasename(relativePath);
		dialogParentPath = projectFileParentPath(relativePath);
		dialogTargetPath = relativePath;
		dialogDestinationPath = '';
		uploadFile = null;
		dialogSubmitting = false;
		dialogError = null;
		dialogOpen = true;
	}

	function compareProjectFolderPaths(a: string, b: string): number {
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
			const parentPath = projectFileParentPath(entry.relativePath);
			childCounts.set(parentPath, (childCounts.get(parentPath) ?? 0) + 1);
		}
		return childCounts;
	}

	function buildFolderDestinationOptions(): FolderDestinationOption[] {
		const childCounts = buildDestinationChildCounts();
		return [
			{
				relativePath: '',
				label: m.project_file_root_destination(),
				depth: 0,
				hasChildren: (childCounts.get('') ?? 0) > 0,
				disabled: false
			},
			...entries
				.filter((candidate) => candidate.isDirectory)
				.sort((a, b) => compareProjectFolderPaths(a.relativePath, b.relativePath))
				.map((candidate) => ({
					relativePath: candidate.relativePath,
					label: projectFileBasename(candidate.relativePath),
					depth: destinationDepth(candidate.relativePath),
					hasChildren: (childCounts.get(candidate.relativePath) ?? 0) > 0,
					disabled: false
				}))
		];
	}

	function buildMoveDestinationOptions(relativePath: string): FolderDestinationOption[] {
		const entry = entryByPath.get(relativePath);
		if (!entry) return [];

		const basename = projectFileBasename(relativePath);
		const currentParentPath = projectFileParentPath(relativePath);
		return buildFolderDestinationOptions().map((candidate) => {
			const targetPath = joinProjectFilePath(candidate.relativePath, basename);
			let reason: string | undefined;
			if (candidate.relativePath === currentParentPath) {
				reason = m.project_file_move_current_location();
			} else if (entry.isDirectory && candidate.relativePath && projectFilePathMatches(candidate.relativePath, relativePath)) {
				reason = m.project_file_move_descendant_blocked();
			} else if (entryByPath.has(targetPath)) {
				reason = m.project_file_move_duplicate_destination();
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
		let parentPath = projectFileParentPath(relativePath);
		while (parentPath) {
			folders[parentPath] = true;
			parentPath = projectFileParentPath(parentPath);
		}
		return folders;
	}

	function isDestinationVisible(relativePath: string): boolean {
		let parentPath = projectFileParentPath(relativePath);
		while (parentPath) {
			if (destinationOpenFolders[parentPath] !== true) return false;
			parentPath = projectFileParentPath(parentPath);
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
		uploadFile = null;
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
		uploadFile = null;
		uploadInputKey += 1;
		dialogSubmitting = false;
		dialogError = null;
		dialogOpen = true;
	}

	function handleUploadFileChange(event: Event) {
		const input = event.currentTarget as HTMLInputElement;
		const file = input.files?.[0] ?? null;
		uploadFile = file;
		if (file) {
			dialogName = file.name;
		}
		dialogError = null;
	}

	async function readUploadFileContent(file: File): Promise<string | null> {
		if (file.size > MAX_MANAGED_PROJECT_FILE_BYTES) {
			dialogError = m.project_file_upload_too_large();
			return null;
		}

		try {
			const bytes = new Uint8Array(await file.arrayBuffer());
			if (bytes.includes(0)) {
				dialogError = m.project_file_upload_text_required();
				return null;
			}
			return new TextDecoder('utf-8', { fatal: true }).decode(bytes);
		} catch {
			dialogError = m.project_file_upload_text_required();
			return null;
		}
	}

	async function handleDialogSubmit() {
		if (dialogMode === 'move') {
			const destination = allDestinationOptions.find((option) => option.relativePath === dialogDestinationPath);
			if (!destination || destination.disabled) {
				dialogError = destination?.reason ?? m.project_file_invalid_move_destination();
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
			const name = validateProjectFileName(dialogName, dialogParentPath, composeFileName);
			if (!name) {
				dialogError = m.project_file_invalid_name();
				return;
			}

			const targetPath = joinProjectFilePath(dialogParentPath, name);
			if (targetPath !== dialogTargetPath && entryByPath.has(targetPath)) {
				dialogError = m.project_file_duplicate_name();
				return;
			}

			onRename?.(dialogTargetPath, name);
			dialogOpen = false;
			return;
		}

		const name = validateProjectFileName(dialogName, dialogDestinationPath, composeFileName);
		if (!name) {
			dialogError = m.project_file_invalid_name();
			return;
		}

		const targetPath = joinProjectFilePath(dialogDestinationPath, name);
		if (entryByPath.has(targetPath)) {
			dialogError = m.project_file_duplicate_name();
			return;
		}

		if (dialogMode === 'upload') {
			if (!uploadFile) {
				dialogError = m.project_file_upload_file_required();
				return;
			}

			dialogSubmitting = true;
			const content = await readUploadFileContent(uploadFile);
			dialogSubmitting = false;
			if (content === null) {
				return;
			}

			onCreateFile?.(dialogDestinationPath, name, content);
			if (dialogDestinationPath) {
				openFolders = {
					...openFolders,
					[dialogDestinationPath]: true
				};
			}
			dialogOpen = false;
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

	function handleDelete(entry: ManagedProjectFileEntry) {
		if (disabled) return;
		openConfirmDialog({
			title: m.delete_name({ name: entry.relativePath }),
			message: m.project_file_delete_confirm({ name: entry.relativePath }),
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
		<span class="text-[11px] font-semibold tracking-wider text-muted-foreground uppercase">{m.project_files()}</span>
		{#if canManageFiles}
			<div class="ml-auto flex items-center gap-0.5">
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
							customLabel={m.project_file_new_file()}
							onclick={() => openCreateDialog('create_file')}
						/>
					</Tooltip.Trigger>
					<Tooltip.Content>{m.project_file_new_file()}</Tooltip.Content>
				</Tooltip.Root>
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
			</div>
		{/if}
	</div>

	{#if readOnlyMessage}
		<div class="border-b border-border px-3 py-2 text-xs text-muted-foreground">{readOnlyMessage}</div>
	{/if}

	<div class="min-h-0 flex-1 overflow-auto">
		<TreeView.Root class="min-w-max p-2 whitespace-nowrap">
			<button
				type="button"
				class={cn(
					'flex w-full items-center gap-1.5 rounded-md px-2 py-1 text-left text-[13px] hover:bg-accent',
					selectedFile === 'compose' && 'bg-accent'
				)}
				onclick={() => onSelect('compose')}
			>
				{#if hasDirectories}
					<span class="inline-flex size-4 shrink-0 items-center justify-center"></span>
				{/if}
				<FileTextIcon class="size-4 shrink-0 text-blue-500" />
				<span class="min-w-0 flex-1 truncate">{composeFileName}</span>
				<span class="inline-flex size-6 shrink-0 items-center justify-center">
					<LockIcon class="size-3.5 shrink-0 text-muted-foreground" aria-label={m.project_file_protected()} />
				</span>
			</button>

			{#if showOverride}
				<button
					type="button"
					class={cn(
						'flex w-full items-center gap-1.5 rounded-md px-2 py-1 text-left text-[13px] hover:bg-accent',
						selectedFile === 'override' && 'bg-accent'
					)}
					onclick={() => onSelect('override')}
				>
					{#if hasDirectories}
						<span class="inline-flex size-4 shrink-0 items-center justify-center"></span>
					{/if}
					<FileTextIcon class="size-4 shrink-0 text-purple-500" />
					<span class="min-w-0 flex-1 truncate">{overrideFileName}</span>
					<span class="inline-flex size-6 shrink-0 items-center justify-center">
						<LockIcon class="size-3.5 shrink-0 text-muted-foreground" aria-label={m.project_file_protected()} />
					</span>
				</button>
			{:else if onAddOverride}
				<button
					type="button"
					class="flex w-full items-center gap-1.5 rounded-md px-2 py-1 text-left text-[13px] text-muted-foreground hover:bg-accent hover:text-foreground"
					onclick={() => onAddOverride?.()}
				>
					{#if hasDirectories}
						<span class="inline-flex size-4 shrink-0 items-center justify-center"></span>
					{/if}
					<CreateFileIcon class="size-4 shrink-0" />
					<span class="min-w-0 flex-1 truncate">{m.compose_override_add()}</span>
				</button>
			{/if}

			<button
				type="button"
				class={cn(
					'flex w-full items-center gap-1.5 rounded-md px-2 py-1 text-left text-[13px] hover:bg-accent',
					selectedFile === 'env' && 'bg-accent'
				)}
				onclick={() => onSelect('env')}
			>
				{#if hasDirectories}
					<span class="inline-flex size-4 shrink-0 items-center justify-center"></span>
				{/if}
				<FileTextIcon class="size-4 shrink-0 text-green-500" />
				<span class="min-w-0 flex-1 truncate">.env</span>
				<span class="inline-flex size-6 shrink-0 items-center justify-center">
					<LockIcon class="size-3.5 shrink-0 text-muted-foreground" aria-label={m.project_file_protected()} />
				</span>
			</button>

			{#if rows.length === 0}
				{#if canManageFiles}
					<div class="px-7 py-3 text-xs text-muted-foreground">{m.project_files_empty()}</div>
				{/if}
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
									? m.project_file_collapse_folder({ name: row.name })
									: m.project_file_expand_folder({ name: row.name })}
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

						{#if canManageFiles}
							<div class="flex shrink-0 items-center gap-0.5">
								<Tooltip.Root>
									<Tooltip.Trigger>
										<button
											type="button"
											class="inline-flex size-6 items-center justify-center rounded text-foreground hover:bg-foreground/10"
											aria-label={m.project_file_rename_label({ name: row.relativePath })}
											{disabled}
											onclick={() => openRenameDialog(row.relativePath)}
										>
											<EditIcon class="size-3.5" />
										</button>
									</Tooltip.Trigger>
									<Tooltip.Content>{m.rename()}</Tooltip.Content>
								</Tooltip.Root>
								<Tooltip.Root>
									<Tooltip.Trigger>
										<button
											type="button"
											class="inline-flex size-6 items-center justify-center rounded text-foreground hover:bg-foreground/10"
											aria-label={m.project_file_move_label({ name: row.relativePath })}
											{disabled}
											onclick={() => openMoveDialog(row.relativePath)}
										>
											<FolderMoveIcon class="size-3.5" />
										</button>
									</Tooltip.Trigger>
									<Tooltip.Content>{m.move()}</Tooltip.Content>
								</Tooltip.Root>
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
						{m.project_file_move_description({ name: dialogTargetPath })}
					{:else if dialogMode === 'upload'}
						{m.project_file_upload_description()}
					{:else if dialogMode === 'create_file' || dialogMode === 'create_folder'}
						{dialogDestinationPath ? m.project_file_parent_path({ path: dialogDestinationPath }) : m.project_file_root_path()}
					{:else}
						{dialogParentPath ? m.project_file_parent_path({ path: dialogParentPath }) : m.project_file_root_path()}
					{/if}
				</Dialog.Description>
			</Dialog.Header>

			{#if dialogMode === 'upload'}
				<div class="space-y-2">
					<Label for="project-file-upload">{m.project_file_upload_file_label()}</Label>
					{#key uploadInputKey}
						<Input id="project-file-upload" type="file" onchange={handleUploadFileChange} aria-invalid={!!dialogError} />
					{/key}
				</div>
			{/if}

			{#if dialogMode !== 'move'}
				<div class="space-y-2">
					<Label for="project-file-name">{m.common_name()}</Label>
					<Input
						id="project-file-name"
						bind:value={dialogName}
						placeholder={dialogMode === 'create_folder'
							? m.project_file_folder_name_placeholder()
							: m.project_file_name_placeholder()}
						aria-invalid={!!dialogError}
					/>
				</div>
			{/if}

			{#if hasDestinationPicker}
				<div class="min-h-0 space-y-2">
					<Label>{m.project_file_move_destination_label()}</Label>
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
											? m.project_file_collapse_folder({ name: option.label })
											: m.project_file_expand_folder({ name: option.label })}
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
						(dialogMode === 'upload' && !uploadFile)}
				/>
			</Dialog.Footer>
		</form>
	</Dialog.Content>
</Dialog.Root>
