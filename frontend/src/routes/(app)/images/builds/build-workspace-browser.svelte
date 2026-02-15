<script lang="ts">
	import FileList from '$lib/components/file-browser/FileList.svelte';
	import FileBreadcrumb from '$lib/components/file-browser/FileBreadcrumb.svelte';
	import CreateFolderDialog from '$lib/components/file-browser/CreateFolderDialog.svelte';
	import FileUploadDialog from '$lib/components/file-browser/FileUploadDialog.svelte';
	import { ArcaneButton } from '$lib/components/arcane-button/index.js';
	import { UploadIcon, MoveToFolderIcon, EllipsisIcon, CopyIcon } from '$lib/icons';
	import { Spinner } from '$lib/components/ui/spinner/index.js';
	import * as DropdownMenu from '$lib/components/ui/dropdown-menu/index.js';
	import * as Dialog from '$lib/components/ui/dialog/index.js';
	import { Textarea } from '$lib/components/ui/textarea';
	import { Label } from '$lib/components/ui/label';
	import { m } from '$lib/paraglide/messages';
	import { toast } from 'svelte-sonner';
	import { UseClipboard } from '$lib/hooks/use-clipboard.svelte';
	import { environmentStore } from '$lib/stores/environment.store.svelte';
	import { queryKeys } from '$lib/query/query-keys';
	import type { FileContentResponse, FileEntry } from '$lib/types/file-browser.type';
	import type { FileProvider } from '$lib/components/file-browser';
	import { createMutation, createQuery, useQueryClient } from '@tanstack/svelte-query';

	let {
		provider,
		rootLabel = '/builds',
		rootPath,
		persistKey = 'arcane-build-workspace-table',
		onSelectContext
	}: {
		provider: FileProvider;
		rootLabel?: string;
		rootPath?: string;
		persistKey?: string;
		onSelectContext?: (path: string) => void;
	} = $props();

	let currentPath = $state('/');
	const envId = $derived(environmentStore.selected?.id || '0');
	const queryClient = useQueryClient();

	const filesQuery = createQuery(() => ({
		queryKey: queryKeys.buildWorkspace.list(envId, currentPath),
		queryFn: () => provider.list(currentPath)
	}));

	const files = $derived.by<FileEntry[]>(() => {
		const result = (filesQuery.data ?? []).slice();
		return result.sort((a, b) => {
			if (a.isDirectory && !b.isDirectory) return -1;
			if (!a.isDirectory && b.isDirectory) return 1;
			return a.name.localeCompare(b.name);
		});
	});

	const loading = $derived(filesQuery.isPending);
	const error = $derived.by(() => {
		const err = filesQuery.error as any;
		if (!err) return null;
		return err.message || 'Failed to load files';
	});

	let showCreateFolder = $state(false);
	let showUpload = $state(false);

	let editorOpen = $state(false);
	let editorFile = $state<FileEntry | null>(null);
	let editorContent = $state('');
	let editorSaving = $state(false);

	const editorContentQuery = createQuery(() => ({
		queryKey: editorFile
			? queryKeys.buildWorkspace.content(envId, editorFile.path)
			: (['build-workspace', envId, 'content', 'none'] as const),
		queryFn: () => provider.getContent(editorFile!.path),
		enabled: !!editorFile && editorOpen,
		onSuccess: (data: FileContentResponse) => {
			editorContent = b64DecodeUnicode(data.content);
		}
	}));

	const editorLoading = $derived(editorContentQuery.isPending || editorContentQuery.isFetching);
	const editorError = $derived.by(() => {
		const err = editorContentQuery.error as any;
		if (!err) return null;
		return err.message || 'Failed to load file';
	});

	const mkdirMutation = createMutation(() => ({
		mutationKey: ['build-workspace', 'mkdir', envId],
		mutationFn: (path: string) => provider.mkdir(path),
		onSuccess: async () => {
			await queryClient.invalidateQueries({ queryKey: queryKeys.buildWorkspace.listPrefix(envId) });
		}
	}));

	const uploadMutation = createMutation(() => ({
		mutationKey: ['build-workspace', 'upload', envId],
		mutationFn: ({ path, file }: { path: string; file: File }) => provider.upload(path, file),
		onSuccess: async () => {
			await queryClient.invalidateQueries({ queryKey: queryKeys.buildWorkspace.listPrefix(envId) });
		}
	}));

	const deleteMutation = createMutation(() => ({
		mutationKey: ['build-workspace', 'delete', envId],
		mutationFn: (path: string) => provider.delete(path),
		onSuccess: async () => {
			await queryClient.invalidateQueries({ queryKey: queryKeys.buildWorkspace.listPrefix(envId) });
		}
	}));

	const downloadMutation = createMutation(() => ({
		mutationKey: ['build-workspace', 'download', envId],
		mutationFn: (path: string) => provider.download(path)
	}));

	const clipboard = new UseClipboard();

	const absoluteCurrentPath = $derived.by(() => {
		const root = (rootPath ?? '').trim();
		if (!root) return currentPath;
		const normalizedRoot = root.endsWith('/') ? root.slice(0, -1) : root;
		if (currentPath === '/' || currentPath === '') return normalizedRoot;
		return `${normalizedRoot}${currentPath.startsWith('/') ? '' : '/'}${currentPath}`;
	});

	function b64DecodeUnicode(str: string) {
		try {
			return decodeURIComponent(
				atob(str)
					.split('')
					.map((c) => '%' + ('00' + c.charCodeAt(0).toString(16)).slice(-2))
					.join('')
			);
		} catch {
			return atob(str);
		}
	}

	function handleNavigate(path: string) {
		currentPath = path;
		onSelectContext?.(path);
	}

	async function handleCopyPath() {
		const status = await clipboard.copy(absoluteCurrentPath);
		if (status === 'success') {
			toast.success('Copied current path');
			return;
		}
		toast.error('Failed to copy path');
	}

	async function openEditor(file: FileEntry) {
		if (file.isDirectory) return;
		editorFile = file;
		editorOpen = true;
		editorContent = '';
	}

	async function handleSaveFile() {
		if (!editorFile) return;
		editorSaving = true;
		try {
			const dirPath = editorFile.path.split('/').slice(0, -1).join('/') || '/';
			const file = new File([editorContent], editorFile.name, { type: 'text/plain' });
			await uploadMutation.mutateAsync({ path: dirPath, file });
			await queryClient.invalidateQueries({ queryKey: queryKeys.buildWorkspace.content(envId, editorFile.path) });
			toast.success('File saved');
			editorOpen = false;
			await filesQuery.refetch();
		} catch (e: any) {
			toast.error(e.message || 'Failed to save file');
		} finally {
			editorSaving = false;
		}
	}
</script>

<div class="flex h-full min-h-0 flex-col gap-5">
	<div class="border-border/40 flex flex-wrap items-start justify-between gap-4 border-b pt-1 pb-4">
		<div class="min-w-0 flex-1 space-y-3">
			<FileBreadcrumb path={currentPath} {rootLabel} onNavigate={handleNavigate} />
			<div class="flex min-w-0 items-center gap-3">
				<div class="text-muted-foreground min-w-0 flex-1 truncate font-mono text-[11px]" title={absoluteCurrentPath}>
					{absoluteCurrentPath}
				</div>
			</div>
		</div>

		<div class="flex shrink-0 flex-wrap items-center justify-end gap-3">
			<DropdownMenu.Root>
				<DropdownMenu.Trigger>
					{#snippet child({ props })}
						<ArcaneButton
							{...props}
							action="base"
							tone="outline"
							size="icon"
							class="size-9"
							icon={EllipsisIcon}
							customLabel={m.common_actions()}
						/>
					{/snippet}
				</DropdownMenu.Trigger>
				<DropdownMenu.Content align="end" class="min-w-[180px]">
					<DropdownMenu.Item onclick={handleCopyPath}>
						<CopyIcon class="size-4" />
						Copy current path
					</DropdownMenu.Item>
					<DropdownMenu.Separator />
					<DropdownMenu.Item onclick={() => (showCreateFolder = true)}>
						<MoveToFolderIcon class="size-4" />
						{m.volumes_browser_new_folder()}
					</DropdownMenu.Item>
					<DropdownMenu.Item onclick={() => (showUpload = true)}>
						<UploadIcon class="size-4" />
						{m.volumes_browser_upload_files()}
					</DropdownMenu.Item>
				</DropdownMenu.Content>
			</DropdownMenu.Root>
		</div>
	</div>

	{#if loading}
		<div class="flex flex-1 items-center justify-center p-8">
			<Spinner class="text-muted-foreground size-8" />
		</div>
	{:else if error}
		<div class="border-destructive/20 bg-destructive/10 text-destructive rounded-lg border p-6 text-sm">
			{error}
		</div>
	{:else}
		<div class="min-h-0 flex-1 overflow-hidden">
			<FileList
				{files}
				{currentPath}
				{persistKey}
				minimal
				onNavigate={handleNavigate}
				onRefresh={() => filesQuery.refetch()}
				onDelete={async (file) => {
					await deleteMutation.mutateAsync(file.path);
					await filesQuery.refetch();
				}}
				onDownload={async (file) => {
					await downloadMutation.mutateAsync(file.path);
				}}
				onPreview={openEditor}
			/>
		</div>
	{/if}
</div>

<CreateFolderDialog
	bind:open={showCreateFolder}
	{currentPath}
	onCreate={async (name) => {
		const fullPath = currentPath === '/' ? `/${name}` : `${currentPath}/${name}`;
		await mkdirMutation.mutateAsync(fullPath);
		await filesQuery.refetch();
	}}
/>

<FileUploadDialog
	bind:open={showUpload}
	{currentPath}
	onUpload={async (file) => {
		await uploadMutation.mutateAsync({ path: currentPath, file });
		await filesQuery.refetch();
	}}
/>

<Dialog.Root bind:open={editorOpen}>
	<Dialog.Content class="max-w-3xl">
		<Dialog.Header>
			<Dialog.Title>{editorFile?.name ?? 'File'}</Dialog.Title>
			<Dialog.Description class="break-all">{editorFile?.path}</Dialog.Description>
		</Dialog.Header>
		<div class="space-y-4 py-2">
			{#if editorLoading}
				<div class="flex items-center justify-center py-8">
					<Spinner class="text-muted-foreground size-8" />
				</div>
			{:else if editorError}
				<div class="border-destructive/20 bg-destructive/10 text-destructive rounded-lg border p-4 text-sm">
					{editorError}
				</div>
			{:else}
				<div class="space-y-2">
					<Label>File contents</Label>
					<Textarea rows={18} bind:value={editorContent} class="font-mono text-xs" />
					<p class="text-muted-foreground text-xs">Saving will overwrite the file contents.</p>
				</div>
			{/if}
		</div>
		<Dialog.Footer class="flex flex-col-reverse gap-2 sm:flex-row sm:justify-end">
			<ArcaneButton action="cancel" tone="outline" type="button" onclick={() => (editorOpen = false)} />
			<ArcaneButton
				action="save"
				type="button"
				onclick={handleSaveFile}
				disabled={editorLoading || editorSaving || !!editorError}
				loading={editorSaving}
			/>
		</Dialog.Footer>
	</Dialog.Content>
</Dialog.Root>
