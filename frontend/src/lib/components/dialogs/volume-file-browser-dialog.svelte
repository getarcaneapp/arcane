<script lang="ts">
	import { ResponsiveDialog } from '$lib/components/ui/responsive-dialog';
	import * as FileBrowser from '$lib/components/ui/file-browser';
	import { Button } from '$lib/components/ui/button';
	import { Checkbox } from '$lib/components/ui/checkbox';
	import { Label } from '$lib/components/ui/label';
	import { volumeService } from '$lib/services/volume-service';
	import type { FileEntry } from '$lib/types/container.type';
	import { m } from '$lib/paraglide/messages';

	let {
		open = $bindable(false),
		volumeName,
		initialPath = '/',
		title
	}: {
		open?: boolean;
		volumeName: string;
		initialPath?: string;
		title?: string;
	} = $props();

	let currentPath = $state('/');
	let files = $state<FileEntry[]>([]);
	let loading = $state(true);
	let error = $state<string | null>(null);

	let selectedFile = $state<FileEntry | null>(null);
	let fileContent = $state<string | undefined>(undefined);
	let fileLoading = $state(false);
	let fileError = $state<string | null>(null);
	let fileBinary = $state(false);
	let fileTruncated = $state(false);
	let showPreview = $state(true);

	async function loadDirectory(path: string) {
		if (!volumeName) return;

		loading = true;
		error = null;
		selectedFile = null;
		fileContent = undefined;

		try {
			const response = await volumeService.browseFiles(volumeName, path);
			files = response.files;
			currentPath = response.path;
		} catch (err) {
			console.error('Failed to browse volume files:', err);
			error = err instanceof Error ? err.message : 'Failed to load directory';
			files = [];
		} finally {
			loading = false;
		}
	}

	async function loadFileContent(file: FileEntry) {
		if (!volumeName || file.type === 'directory') return;

		fileLoading = true;
		fileError = null;
		fileContent = undefined;
		fileBinary = false;
		fileTruncated = false;

		try {
			const response = await volumeService.getFileContent(volumeName, file.path);
			fileContent = response.content;
			fileBinary = response.isBinary;
			fileTruncated = response.truncated;
		} catch (err) {
			console.error('Failed to read file:', err);
			fileError = err instanceof Error ? err.message : 'Failed to read file';
		} finally {
			fileLoading = false;
		}
	}

	function handleNavigate(path: string) {
		loadDirectory(path);
	}

	function handleSelect(file: FileEntry) {
		if (file.type !== 'directory') {
			selectedFile = file;
			loadFileContent(file);
		}
	}

	function handleOpen(file: FileEntry) {
		selectedFile = null;
		fileContent = undefined;
		fileError = null;
		loadDirectory(file.path);
	}

	function handleRetry() {
		loadDirectory(currentPath);
	}

	$effect(() => {
		if (open && volumeName) {
			currentPath = initialPath;
			loadDirectory(initialPath);
		}
	});
</script>

<ResponsiveDialog
	bind:open
	title={title || m.file_browser_title()}
	description={m.file_browser_description()}
	contentClass="sm:max-w-3xl lg:max-w-5xl"
>
	{#snippet children()}
		<div class="flex h-[60vh] flex-col">
			<div class="mb-2 flex items-center justify-end">
				<div class="hidden items-center gap-2 lg:flex">
					<Checkbox id="volume-dialog-show-preview" bind:checked={showPreview} />
					<Label for="volume-dialog-show-preview" class="cursor-pointer text-sm">{m.file_browser_show_preview()}</Label>
				</div>
			</div>

			<FileBrowser.Root class="flex-1" {loading} {error}>
				<FileBrowser.Breadcrumb path={currentPath} onNavigate={handleNavigate} />

				<div class="flex flex-1 overflow-hidden">
					<div class="flex min-w-0 flex-1 flex-col">
						{#if loading}
							<FileBrowser.Loading />
						{:else if error}
							<FileBrowser.Error message={error} onRetry={handleRetry} />
						{:else if files.length === 0}
							<FileBrowser.Empty />
						{:else}
							<FileBrowser.List {files} selectedPath={selectedFile?.path} onSelect={handleSelect} onOpen={handleOpen} />
						{/if}
					</div>

					{#if showPreview && selectedFile}
						<FileBrowser.Preview
							class="hidden w-1/2 max-w-md lg:flex"
							file={selectedFile}
							content={fileContent}
							loading={fileLoading}
							isBinary={fileBinary}
							truncated={fileTruncated}
							error={fileError}
						/>
					{/if}
				</div>
			</FileBrowser.Root>
		</div>
	{/snippet}

	{#snippet footer()}
		<Button variant="outline" onclick={() => (open = false)}>
			{m.common_close()}
		</Button>
	{/snippet}
</ResponsiveDialog>
