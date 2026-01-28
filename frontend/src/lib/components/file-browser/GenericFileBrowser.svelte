<script lang="ts" module>
	import type { BackupEntry, FileEntry } from '$lib/types/file-browser.type';

	export interface FileProvider {
		list: (path: string) => Promise<FileEntry[]>;
		mkdir: (path: string) => Promise<void>;
		upload: (path: string, file: File) => Promise<void>;
		delete: (path: string) => Promise<void>;
		download: (path: string) => Promise<void>;
		getContent: (path: string) => Promise<{ content: string }>;
		listBackups?: () => Promise<BackupEntry[]>;
		restoreFromBackup?: (backupId: string, path: string) => Promise<void>;
		backupHasPath?: (backupId: string, path: string) => Promise<boolean>;
	}
</script>

<script lang="ts">
	import { onMount } from 'svelte';
	import FileList from './FileList.svelte';
	import FileBreadcrumb from './FileBreadcrumb.svelte';
	import { UploadIcon, MoveToFolderIcon, InfoIcon } from '$lib/icons';
	import { ArcaneButton } from '$lib/components/arcane-button';
	import CreateFolderDialog from './CreateFolderDialog.svelte';
	import FileUploadDialog from './FileUploadDialog.svelte';
	import FilePreview from './FilePreview.svelte';
	import * as m from '$lib/paraglide/messages.js';
	import { Spinner } from '$lib/components/ui/spinner/index.js';
	import { ResponsiveDialog } from '$lib/components/ui/responsive-dialog';
	import * as Select from '$lib/components/ui/select';
	import * as Alert from '$lib/components/ui/alert';
	import { Label } from '$lib/components/ui/label';
	import { toast } from 'svelte-sonner';
	import bytes from 'bytes';
	import { format } from 'date-fns';

	let { provider, rootLabel, persistKey }: { provider: FileProvider; rootLabel?: string; persistKey?: string } = $props();

	let currentPath = $state('/');
	let files = $state<FileEntry[]>([]);
	let loading = $state(true);
	let error = $state<string | null>(null);

	let showCreateFolder = $state(false);
	let showUpload = $state(false);
	let previewFile = $state<FileEntry | null>(null);
	let showRestoreFile = $state(false);
	let restoreTarget = $state<FileEntry | null>(null);
	let backups = $state<BackupEntry[]>([]);
	let loadingBackups = $state(false);
	let restoringFile = $state(false);
	let selectedBackupId = $state('');
	let backupHasFile = $state<boolean | null>(null);
	let checkingBackup = $state(false);
	let lastCheckedKey = $state('');
	const selectedBackup = $derived(backups.find((b) => b.id === selectedBackupId));

	const canRestoreFromBackup = $derived(!!provider.listBackups && !!provider.restoreFromBackup);
	const requiresBackupCheck = $derived(!!provider.backupHasPath);

	async function loadFiles(path: string) {
		loading = true;
		error = null;
		try {
			const result = await provider.list(path);
			// Sort: directories first, then alphabetically
			files = result.sort((a, b) => {
				if (a.isDirectory && !b.isDirectory) return -1;
				if (!a.isDirectory && b.isDirectory) return 1;
				return a.name.localeCompare(b.name);
			});
			currentPath = path;
		} catch (e: any) {
			error = e.message || m.common_failed();
		} finally {
			loading = false;
		}
	}

	function handleNavigate(path: string) {
		loadFiles(path);
	}

	async function loadBackups() {
		if (!provider.listBackups) return;
		loadingBackups = true;
		try {
			backups = await provider.listBackups();
			if (backups.length > 0) {
				selectedBackupId = backups[0].id;
			}
		} catch (e: any) {
			toast.error(e.message || m.common_failed());
		} finally {
			loadingBackups = false;
		}
	}

	function openRestoreFileDialog(file: FileEntry) {
		if (file.isDirectory || file.isSymlink) return;
		restoreTarget = file;
		selectedBackupId = '';
		backups = [];
		backupHasFile = null;
		lastCheckedKey = '';
		showRestoreFile = true;
		loadBackups();
	}

	async function handleRestoreFile() {
		if (!restoreTarget || !provider.restoreFromBackup || !selectedBackupId) return;
		restoringFile = true;
		try {
			await provider.restoreFromBackup(selectedBackupId, restoreTarget.path);
			toast.success(m.volumes_backup_file_restore_success());
			showRestoreFile = false;
			// Refresh the file list to show the restored file
			await loadFiles(currentPath);
		} catch (e: any) {
			toast.error(e.message || m.common_failed());
		} finally {
			restoringFile = false;
		}
	}

	async function checkBackupHasFile(backupId: string, filePath: string) {
		if (!provider.backupHasPath) return;
		const key = `${backupId}:${filePath}`;
		if (key === lastCheckedKey) return;
		lastCheckedKey = key;
		checkingBackup = true;
		backupHasFile = null;
		try {
			backupHasFile = await provider.backupHasPath(backupId, filePath);
		} catch (e: any) {
			backupHasFile = null;
			toast.error(e.message || m.common_failed());
		} finally {
			checkingBackup = false;
		}
	}

	function formatBackupLabel(backup: BackupEntry): string {
		const sizeLabel = bytes.format(backup.size, { unitSeparator: ' ' }) ?? '-';
		const createdLabel = backup.createdAt ? format(new Date(backup.createdAt), 'PP p') : '-';
		return `${backup.id} • ${createdLabel} • ${sizeLabel}`;
	}

	onMount(() => {
		loadFiles('/');
	});

	$effect(() => {
		if (!showRestoreFile) return;
		if (!requiresBackupCheck) return;
		if (!restoreTarget || !selectedBackupId) {
			backupHasFile = null;
			return;
		}
		checkBackupHasFile(selectedBackupId, restoreTarget.path);
	});
</script>

<div class="flex flex-col gap-4">
	<div class="flex items-center justify-between">
		<FileBreadcrumb path={currentPath} {rootLabel} onNavigate={handleNavigate} />
		<div class="flex gap-2">
			<ArcaneButton
				action="base"
				tone="outline"
				size="sm"
				onclick={() => (showCreateFolder = true)}
				icon={MoveToFolderIcon}
				customLabel={m.volumes_browser_new_folder()}
			/>
			<ArcaneButton
				action="base"
				tone="outline"
				size="sm"
				onclick={() => (showUpload = true)}
				icon={UploadIcon}
				customLabel={m.volumes_browser_upload_files()}
			/>
		</div>
	</div>

	{#if loading}
		<div class="flex justify-center p-12">
			<Spinner class="text-muted-foreground size-8" />
		</div>
	{:else if error}
		<div class="border-destructive/20 bg-destructive/10 text-destructive rounded-lg border p-8 text-center">
			{error}
		</div>
	{:else}
		<FileList
			{files}
			{currentPath}
			{persistKey}
			onNavigate={handleNavigate}
			onRefresh={() => loadFiles(currentPath)}
			onDelete={(file) => provider.delete(file.path)}
			onDownload={(file) => provider.download(file.path)}
			onPreview={(file) => (previewFile = file)}
			onRestoreFromBackup={canRestoreFromBackup ? openRestoreFileDialog : undefined}
		/>
	{/if}
</div>

<CreateFolderDialog
	bind:open={showCreateFolder}
	{currentPath}
	onCreate={async (name) => {
		const fullPath = currentPath === '/' ? `/${name}` : `${currentPath}/${name}`;
		await provider.mkdir(fullPath);
		await loadFiles(currentPath);
	}}
/>

<FileUploadDialog
	bind:open={showUpload}
	{currentPath}
	onUpload={async (file) => {
		await provider.upload(currentPath, file);
		await loadFiles(currentPath);
	}}
/>

{#if previewFile}
	<FilePreview
		file={previewFile}
		fetchContent={async (path) => {
			const res = await provider.getContent(path);
			return res.content;
		}}
		onClose={() => (previewFile = null)}
	/>
{/if}

<ResponsiveDialog
	bind:open={showRestoreFile}
	title="Restore file from backup"
	description="Select a backup to restore the chosen file."
	contentClass="sm:max-w-[520px]"
>
	{#snippet children()}
		<div class="space-y-4 py-2">
			<Alert.Root class="py-2 [&>svg]:top-2">
				<InfoIcon class="size-4" />
				<Alert.Description class="text-xs">
					{m.volumes_backup_safety_info()}
				</Alert.Description>
			</Alert.Root>

			{#if restoreTarget}
				<div class="text-muted-foreground text-xs">
					File: <code class="bg-muted/40 rounded px-1.5 py-0.5 font-mono">{restoreTarget.path}</code>
				</div>
			{/if}

			{#if loadingBackups}
				<div class="text-muted-foreground flex items-center gap-2 text-sm">
					<Spinner class="size-4" />
					Loading backups...
				</div>
			{:else if backups.length === 0}
				<div class="text-muted-foreground text-sm">No backups available.</div>
			{:else}
				<div class="space-y-2">
					<Label for="restore-backup-select">Backup</Label>
					<div class="w-full overflow-hidden">
						<Select.Root
							type="single"
							value={selectedBackupId}
							onValueChange={(value) => {
								selectedBackupId = value;
							}}
						>
							<Select.Trigger id="restore-backup-select" class="h-10 w-full overflow-hidden">
								<span class="min-w-0 flex-1 truncate">
									{selectedBackup ? formatBackupLabel(selectedBackup) : 'Select backup'}
								</span>
							</Select.Trigger>
							<Select.Content>
								{#each backups as backup (backup.id)}
									<Select.Item value={backup.id}>{formatBackupLabel(backup)}</Select.Item>
								{/each}
							</Select.Content>
						</Select.Root>
					</div>
				</div>
				{#if requiresBackupCheck}
					{#if checkingBackup}
						<div class="text-muted-foreground flex items-center gap-2 text-xs">
							<Spinner class="size-3" />
							Checking backup contents...
						</div>
					{:else if backupHasFile === false}
						<div class="text-destructive text-xs">This backup does not contain the selected file.</div>
					{/if}
				{/if}
			{/if}
		</div>
	{/snippet}

	{#snippet footer()}
		<ArcaneButton
			action="cancel"
			onclick={() => {
				showRestoreFile = false;
				restoreTarget = null;
				selectedBackupId = '';
			}}
		/>
		<ArcaneButton
			action="create"
			customLabel="Restore file"
			onclick={handleRestoreFile}
			loading={restoringFile}
			disabled={restoringFile ||
				loadingBackups ||
				checkingBackup ||
				!selectedBackupId ||
				!restoreTarget ||
				(requiresBackupCheck && backupHasFile !== true)}
		/>
	{/snippet}
</ResponsiveDialog>
