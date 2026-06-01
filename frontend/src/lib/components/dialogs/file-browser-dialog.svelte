<script lang="ts">
	import { ResponsiveDialog } from '$lib/components/ui/responsive-dialog/index.js';
	import { Button } from '$lib/components/ui/button/index.js';
	import { Spinner } from '$lib/components/ui/spinner/index.js';
	import { ScrollArea } from '$lib/components/ui/scroll-area/index.js';
	import { gitRepositoryService } from '$lib/services/git-repository-service';
	import { queryKeys } from '$lib/query/query-keys';
	import type { FileTreeNode } from '$lib/types/automation';
	import { FolderOpenIcon, FileTextIcon, ArrowRightIcon } from '$lib/icons';
	import { m } from '$lib/paraglide/messages';
	import { createQuery } from '@tanstack/svelte-query';
	import type { Snippet } from 'svelte';

	type FileBrowserDialogProps = {
		open: boolean;
		repositoryId: string;
		branch: string;
		onSelect: (filePath: string) => void;
		// title/description override the dialog heading. Defaults are generic so the
		// component carries no domain assumption (compose vs script vs anything else).
		title?: string;
		description?: string;
		// rootPath scopes browsing to a subdirectory of the repo. Navigation cannot
		// escape this root, the breadcrumb hides everything above it, and the
		// onSelect callback receives a path relative to this root. Use this when
		// the selectable file must live inside a specific subtree (e.g. a script
		// must live inside the sync directory so the deploy-time runner can find
		// it). Default empty = full repo.
		rootPath?: string;
		// fileFilter decides which files are selectable. Directories are
		// always navigable. Default accepts everything; callers narrow it.
		fileFilter?: (file: FileTreeNode) => boolean;
		// Optional UI decorations rendered per file (badge) and beneath the
		// list (hint). Callers compose these to add domain-specific affordances
		// (e.g. a "COMPOSE" badge + an explanatory hint) without the dialog
		// knowing anything about the domain.
		fileBadge?: Snippet<[FileTreeNode]>;
		footerHint?: Snippet;
	};

	let {
		open = $bindable(false),
		repositoryId,
		branch,
		onSelect,
		title = m.git_sync_browse_files_title(),
		description = m.git_sync_browse_files_description(),
		rootPath = '',
		fileFilter = () => true,
		fileBadge,
		footerHint
	}: FileBrowserDialogProps = $props();

	// Internal navigation is stored as a path relative to the dialog's root, so the
	// query key and breadcrumb stay consistent the moment the dialog opens — there's
	// no race between "set currentPath" and "query enables when open=true".
	let userPath = $state('');
	const normalizedRoot = $derived(rootPath.replace(/^\/+|\/+$/g, ''));
	const currentPath = $derived(joinPath(normalizedRoot, userPath));
	const pathSegments = $derived(userPath.split('/').filter(Boolean));
	const fileTreeQuery = createQuery<{ files: FileTreeNode[] }>(() => ({
		queryKey: queryKeys.gitRepositories.files(repositoryId, branch, currentPath),
		queryFn: () => gitRepositoryService.browseFiles(repositoryId, branch, currentPath),
		enabled: open && !!repositoryId && !!branch,
		staleTime: 0
	}));
	const files = $derived(fileTreeQuery.data?.files ?? []);
	const loading = $derived(fileTreeQuery.isPending || fileTreeQuery.isFetching);
	const atRoot = $derived(userPath === '');

	function joinPath(a: string, b: string): string {
		if (!a) return b;
		if (!b) return a;
		return `${a}/${b}`;
	}

	function pathRelativeToRoot(absolutePath: string): string {
		if (!normalizedRoot) return absolutePath;
		if (absolutePath === normalizedRoot) return '';
		if (absolutePath.startsWith(normalizedRoot + '/')) {
			return absolutePath.slice(normalizedRoot.length + 1);
		}
		return absolutePath;
	}

	function handleFileClick(file: FileTreeNode) {
		if (file.type === 'directory') {
			userPath = pathRelativeToRoot(file.path);
			return;
		}
		if (fileFilter(file)) {
			onSelect(pathRelativeToRoot(file.path));
			open = false;
		}
	}

	function goToPath(index: number) {
		userPath = pathSegments.slice(0, index + 1).join('/');
	}

	function goBack() {
		userPath = pathSegments.slice(0, -1).join('/');
	}
</script>

<ResponsiveDialog
	bind:open
	onOpenChange={(isOpen) => {
		if (isOpen && repositoryId && branch) {
			userPath = '';
		}
	}}
	{title}
	{description}
	contentClass="max-w-2xl"
>
	<div class="space-y-4">
		<!-- Breadcrumb navigation. "Root" is the dialog's configured root (or the
		     repo root when no rootPath was passed). Navigation stops there. -->
		<div class="flex items-center gap-2 text-sm">
			<Button variant="ghost" size="sm" onclick={() => (userPath = '')} disabled={atRoot} class="h-8 px-2">
				<FolderOpenIcon class="size-4" />
				<span class="ml-1">{normalizedRoot || m.git_sync_browse_root()}</span>
			</Button>
			{#each pathSegments as segment, index (`${index}-${segment}`)}
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
					{#if !atRoot}
						<button
							onclick={goBack}
							class="hover:bg-accent flex w-full items-center gap-2 rounded-md px-3 py-2 text-left transition-colors"
						>
							<FolderOpenIcon class="text-muted-foreground size-4" />
							<span class="text-sm">..</span>
						</button>
					{/if}
					{#each files as file (file.path)}
						{@const canSelect = file.type === 'directory' || fileFilter(file)}
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
							<span class="text-sm">{file.name}</span>
							{@render fileBadge?.(file)}
						</button>
					{/each}
				</div>
			{/if}
		</ScrollArea>

		{@render footerHint?.()}
	</div>

	{#snippet footer()}
		<Button variant="outline" onclick={() => (open = false)}>
			{m.common_cancel()}
		</Button>
	{/snippet}
</ResponsiveDialog>
