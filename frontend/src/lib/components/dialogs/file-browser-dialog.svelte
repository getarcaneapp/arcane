<script lang="ts">
	import { ResponsiveDialog } from '$lib/components/ui/responsive-dialog/index.js';
	import { Button } from '$lib/components/ui/button/index.js';
	import { Spinner } from '$lib/components/ui/spinner/index.js';
	import { ScrollArea } from '$lib/components/ui/scroll-area/index.js';
	import { gitRepositoryService } from '$lib/services/git-repository-service';
	import type { FileTreeNode } from '$lib/types/gitops.type';
	import { FolderOpenIcon, FileTextIcon, ArrowRightIcon } from '$lib/icons';
	import { m } from '$lib/paraglide/messages';

	type FileBrowserDialogProps = {
		open: boolean;
		repositoryId: string;
		branch: string;
		onSelect: (filePath: string) => void;
	};

	let { open = $bindable(false), repositoryId, branch, onSelect }: FileBrowserDialogProps = $props();

	let currentPath = $state('');
	let files = $state<FileTreeNode[]>([]);
	let loading = $state(false);
	let pathSegments = $derived(currentPath.split('/').filter(Boolean));

	async function loadFiles(path: string = '') {
		if (!repositoryId || !branch) return;

		loading = true;
		try {
			const result = await gitRepositoryService.browseFiles(repositoryId, branch, path);
			files = result.files || [];
			currentPath = path;
		} catch (error) {
			console.error('Failed to load files:', error);
			files = [];
		} finally {
			loading = false;
		}
	}

	function handleFileClick(file: FileTreeNode) {
		if (file.type === 'directory') {
			loadFiles(file.path);
		} else {
			// Only allow selecting compose files
			if (file.name.endsWith('.yml') || file.name.endsWith('.yaml')) {
				onSelect(file.path);
				open = false;
			}
		}
	}

	function goToPath(index: number) {
		const newPath = pathSegments.slice(0, index + 1).join('/');
		loadFiles(newPath);
	}

	function goBack() {
		const segments = pathSegments.slice(0, -1);
		loadFiles(segments.join('/'));
	}

	$effect(() => {
		if (open && repositoryId && branch) {
			loadFiles('');
		}
	});

	function isComposeFile(fileName: string): boolean {
		return fileName.endsWith('.yml') || fileName.endsWith('.yaml');
	}
</script>

<ResponsiveDialog
	bind:open
	title={m.git_sync_browse_files_title()}
	description={m.git_sync_browse_files_description()}
	contentClass="max-w-2xl"
>
	{#snippet children()}
		<div class="space-y-4">
			<!-- Breadcrumb navigation -->
			<div class="flex items-center gap-2 text-sm">
				<Button variant="ghost" size="sm" onclick={() => loadFiles('')} disabled={currentPath === ''} class="h-8 px-2">
					<FolderOpenIcon class="size-4" />
					<span class="ml-1">{m.git_sync_browse_root()}</span>
				</Button>
				{#each pathSegments as segment, index}
					<ArrowRightIcon class="text-muted-foreground size-4" />
					<Button variant="ghost" size="sm" onclick={() => goToPath(index)} class="h-8 px-2">
						{segment}
					</Button>
				{/each}
			</div>

			<!-- File list -->
			<ScrollArea class="h-96 rounded-md border">
				{#if loading}
					<div class="flex items-center justify-center py-8">
						<Spinner class="size-6" />
					</div>
				{:else if files.length === 0}
					<div class="text-muted-foreground flex items-center justify-center py-8 text-sm">{m.git_sync_browse_no_files()}</div>
				{:else}
					<div class="space-y-1 p-2">
						{#if currentPath !== ''}
							<button
								onclick={goBack}
								class="hover:bg-accent flex w-full items-center gap-2 rounded-md px-3 py-2 text-left transition-colors"
							>
								<FolderOpenIcon class="text-muted-foreground size-4" />
								<span class="text-sm">..</span>
							</button>
						{/if}
						{#each files as file}
							{@const isCompose = file.type === 'file' && isComposeFile(file.name)}
							{@const canSelect = file.type === 'directory' || isCompose}
							<button
								onclick={() => handleFileClick(file)}
								disabled={!canSelect}
								class="hover:bg-accent flex w-full items-center gap-2 rounded-md px-3 py-2 text-left transition-colors disabled:cursor-not-allowed disabled:opacity-50"
							>
								{#if file.type === 'directory'}
									<FolderOpenIcon class="size-4 text-blue-500" />
								{:else}
									<FileTextIcon class="text-muted-foreground size-4" />
								{/if}
								<span class="text-sm {isCompose ? 'font-medium' : ''}">
									{file.name}
								</span>
								{#if isCompose}
									<span class="bg-primary/10 text-primary ml-auto rounded px-2 py-0.5 text-xs">
										{m.git_sync_browse_compose_label()}
									</span>
								{/if}
							</button>
						{/each}
					</div>
				{/if}
			</ScrollArea>

			<p class="text-muted-foreground text-xs">
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
