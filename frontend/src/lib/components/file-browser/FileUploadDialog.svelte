<script lang="ts">
	import * as Dialog from '$lib/components/ui/dialog';
	import { ArcaneButton } from '$lib/components/arcane-button';
	import { FileDropZone, displaySize, MEGABYTE, type FileDropZoneProps } from '$lib/components/ui/file-drop-zone';
	import { toast } from 'svelte-sonner';
	import * as m from '$lib/paraglide/messages.js';
	import { CloseIcon } from '$lib/icons';

	let {
		open = $bindable(false),
		currentPath,
		onUpload: onUploadAction
	}: {
		open: boolean;
		currentPath: string;
		onUpload: (file: File) => Promise<void>;
	} = $props();

	let files = $state<File[]>([]);
	let uploading = $state(false);

	const onUpload: FileDropZoneProps['onUpload'] = async (newFiles) => {
		files = [...files, ...newFiles];
	};

	const onFileRejected: FileDropZoneProps['onFileRejected'] = ({ reason, file }) => {
		toast.error(`${file.name} failed to upload!`, { description: reason });
	};

	function removeFile(index: number) {
		files = files.filter((_, i) => i !== index);
	}

	async function handleUpload() {
		if (files.length === 0) return;
		uploading = true;
		try {
			for (const file of files) {
				await onUploadAction(file);
			}
			toast.success(m.common_success());
			open = false;
			files = [];
		} catch (e: any) {
			toast.error(e.message || m.common_failed());
		} finally {
			uploading = false;
		}
	}
</script>

<Dialog.Root
	bind:open
	onOpenChange={(isOpen) => {
		if (!isOpen) files = [];
	}}
>
	<Dialog.Content class="sm:max-w-[500px]">
		<Dialog.Header>
			<Dialog.Title>{m.volumes_browser_upload_files()}</Dialog.Title>
			<Dialog.Description>
				Upload files to {currentPath}
			</Dialog.Description>
		</Dialog.Header>
		<div class="space-y-4 py-4">
			<FileDropZone {onUpload} {onFileRejected} multiple fileCount={files.length} disabled={uploading} />

			{#if files.length > 0}
				<div class="flex max-h-[200px] flex-col gap-2 overflow-y-auto pr-1">
					{#each files as file, i (file.name + i)}
						<div class="border-border bg-muted/50 flex items-center justify-between gap-2 rounded-lg border p-2">
							<div class="flex flex-col overflow-hidden">
								<span class="truncate text-xs font-medium">{file.name}</span>
								<span class="text-muted-foreground text-[10px]">{displaySize(file.size)}</span>
							</div>
							<ArcaneButton
								action="base"
								tone="ghost"
								size="icon"
								class="h-6 w-6"
								onclick={() => removeFile(i)}
								icon={CloseIcon}
								disabled={uploading}
							/>
						</div>
					{/each}
				</div>
			{/if}
		</div>
		<Dialog.Footer>
			<ArcaneButton
				action="cancel"
				onclick={() => {
					open = false;
					files = [];
				}}
				disabled={uploading}
			/>
			<ArcaneButton
				action="create"
				onclick={handleUpload}
				disabled={uploading || files.length === 0}
				loading={uploading}
				customLabel={uploading ? m.common_action_updating() : m.common_update()}
			/>
		</Dialog.Footer>
	</Dialog.Content>
</Dialog.Root>
