<script lang="ts">
	import * as Card from '$lib/components/ui/card';
	import * as FileBrowser from '$lib/components/ui/file-browser';
	import { Checkbox } from '$lib/components/ui/checkbox';
	import { Label } from '$lib/components/ui/label';
	import { containerService } from '$lib/services/container-service';
	import type { FileEntry } from '$lib/types/container.type';
	import { m } from '$lib/paraglide/messages';
	import { FolderOpenIcon } from '$lib/icons';
	import { onMount } from 'svelte';

	let {
		containerId
	}: {
		containerId: string | undefined;
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
		if (!containerId) return;

		loading = true;
		error = null;
		selectedFile = null;
		fileContent = undefined;

		try {
			const response = await containerService.browseFiles(containerId, path);
			files = response.files;
			currentPath = response.path;
		} catch (err) {
			console.error('Failed to browse container files:', err);
			error = err instanceof Error ? err.message : 'Failed to load directory';
			files = [];
		} finally {
			loading = false;
		}
	}

	async function loadFileContent(file: FileEntry) {
		if (!containerId || file.type === 'directory') return;

		fileLoading = true;
		fileError = null;
		fileContent = undefined;
		fileBinary = false;
		fileTruncated = false;

		try {
			const response = await containerService.getFileContent(containerId, file.path);
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
		// Only files can be selected for preview
		if (file.type !== 'directory') {
			selectedFile = file;
			loadFileContent(file);
		}
	}

	function handleOpen(file: FileEntry) {
		// Clear selection when navigating into a directory
		selectedFile = null;
		fileContent = undefined;
		fileError = null;
		loadDirectory(file.path);
	}

	function handleRetry() {
		loadDirectory(currentPath);
	}

	onMount(() => {
		loadDirectory('/');
	});

	$effect(() => {
		if (containerId) {
			loadDirectory('/');
		}
	});
</script>

<Card.Root class="h-full">
	<Card.Header icon={FolderOpenIcon}>
		<div class="flex flex-1 flex-col gap-1.5 sm:flex-row sm:items-start sm:justify-between">
			<div class="flex flex-col gap-1.5">
				<Card.Title>
					<h2>{m.file_browser_title()}</h2>
				</Card.Title>
				<Card.Description>{m.file_browser_description()}</Card.Description>
			</div>
			<div class="hidden items-center gap-2 lg:flex">
				<Checkbox id="show-preview" bind:checked={showPreview} />
				<Label for="show-preview" class="cursor-pointer text-sm">{m.file_browser_show_preview()}</Label>
			</div>
		</div>
	</Card.Header>
	<Card.Content class="h-[calc(100vh-320px)] p-0">
		<FileBrowser.Root class="h-full border-0" {loading} {error}>
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
						<FileBrowser.List
							{files}
							selectedPath={selectedFile?.path}
							onSelect={handleSelect}
							onOpen={handleOpen}
						/>
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
	</Card.Content>
</Card.Root>
