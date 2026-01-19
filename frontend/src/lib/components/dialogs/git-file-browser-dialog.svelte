<script lang="ts">
	import { ResponsiveDialog } from '$lib/components/ui/responsive-dialog';
	import * as FileBrowser from '$lib/components/ui/file-browser';
	import { Button } from '$lib/components/ui/button';
	import { FolderOpenIcon, FileTextIcon } from '$lib/icons';
	import { gitRepositoryService } from '$lib/services/git-repository-service';
	import type { FileTreeNode } from '$lib/types/gitops.type';
	import { m } from '$lib/paraglide/messages';

	type GitFileBrowserDialogProps = {
		open: boolean;
		repositoryId: string;
		branch: string;
		onSelect: (filePath: string) => void;
	};

	let { open = $bindable(false), repositoryId, branch, onSelect }: GitFileBrowserDialogProps = $props();

	let currentPath = $state('');
	let files = $state<FileTreeNode[]>([]);
	let loading = $state(false);
	let error = $state<string | null>(null);

	function isComposeFile(fileName: string): boolean {
		return fileName.endsWith('.yml') || fileName.endsWith('.yaml');
	}

	async function loadFiles(path: string = '') {
		if (!repositoryId || !branch) return;

		loading = true;
		error = null;

		try {
			const result = await gitRepositoryService.browseFiles(repositoryId, branch, path);
			files = result.files || [];
			currentPath = path;
		} catch (err) {
			console.error('Failed to load files:', err);
			error = err instanceof Error ? err.message : 'Failed to load files';
			files = [];
		} finally {
			loading = false;
		}
	}

	function handleNavigate(path: string) {
		if (path === '/') {
			loadFiles('');
		} else {
			loadFiles(path.startsWith('/') ? path.slice(1) : path);
		}
	}

	function handleSelect(file: FileTreeNode) {
		if (file.type !== 'directory' && isComposeFile(file.name)) {
			onSelect(file.path);
			open = false;
		}
	}

	function handleOpen(file: FileTreeNode) {
		if (file.type === 'directory') {
			loadFiles(file.path);
		}
	}

	function handleRetry() {
		loadFiles(currentPath);
	}

	$effect(() => {
		if (open && repositoryId && branch) {
			loadFiles('');
		}
	});
</script>

{#snippet fileIcon({ file }: { file: FileTreeNode })}
	{#if file.type === 'directory'}
		<FolderOpenIcon class="size-4 text-blue-500" />
	{:else if isComposeFile(file.name)}
		<FileTextIcon class="text-primary size-4" />
	{:else}
		<FileTextIcon class="text-muted-foreground size-4 opacity-50" />
	{/if}
{/snippet}

<ResponsiveDialog
	bind:open
	title={m.git_sync_browse_files_title()}
	description={m.git_sync_browse_files_description()}
	contentClass="max-w-2xl"
>
	{#snippet children()}
		<div class="flex h-[400px] flex-col">
			<FileBrowser.Root class="flex-1" {loading} {error}>
				<FileBrowser.Breadcrumb path={'/' + currentPath} onNavigate={handleNavigate} />

				<div class="flex flex-1 overflow-hidden">
					<div class="flex min-w-0 flex-1 flex-col">
						{#if loading}
							<FileBrowser.Loading />
						{:else if error}
							<FileBrowser.Error message={error} onRetry={handleRetry} />
						{:else if files.length === 0}
							<FileBrowser.Empty message={m.git_sync_browse_no_files()} />
						{:else}
							<FileBrowser.List {files} onSelect={handleSelect} onOpen={handleOpen} icon={fileIcon} />
						{/if}
					</div>
				</div>
			</FileBrowser.Root>

			<p class="text-muted-foreground mt-3 text-xs">
				{m.git_sync_browse_hint()}
			</p>
		</div>
	{/snippet}

	{#snippet footer()}
		<Button variant="outline" onclick={() => (open = false)}>
			{m.common_cancel()}
		</Button>
	{/snippet}
</ResponsiveDialog>
