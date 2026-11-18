<script lang="ts">
	import { useId } from 'bits-ui';
	import UpdateIcon from 'virtual:icons/solar/server-square-update-bold';

	import { getUiLabels } from '#lib/labels.js';
	import { cn } from '#lib/utils.js';

	import { displaySize } from './display-size';
	import type { FileDropZoneProps, FileRejectedReason } from './types';

	let {
		id = useId(),
		children,
		maxFiles,
		maxFileSize,
		fileCount,
		disabled = false,
		multiple,
		onUpload,
		onFileRejected,
		accept,
		class: className,
		...rest
	}: FileDropZoneProps = $props();

	let uploading = $state(false);

	const drop = async (
		e: DragEvent & {
			currentTarget: EventTarget & HTMLLabelElement;
		}
	) => {
		if (disabled || !canUploadFiles) return;

		e.preventDefault();

		const droppedFiles = Array.from(e.dataTransfer?.files ?? []);

		await upload(droppedFiles);
	};

	const change = async (
		e: Event & {
			currentTarget: EventTarget & HTMLInputElement;
		}
	) => {
		if (disabled) return;

		const selectedFiles = e.currentTarget.files;

		if (!selectedFiles) return;

		await upload(Array.from(selectedFiles));

		// this if a file fails and we upload the same file again we still get feedback
		(e.target as HTMLInputElement).value = '';
	};

	const shouldAcceptFile = (file: File, fileNumber: number): FileRejectedReason | undefined => {
		if (maxFileSize !== undefined && file.size > maxFileSize) return 'Maximum file size exceeded';

		if (maxFiles !== undefined && fileNumber > maxFiles) return 'Maximum files uploaded';

		if (!accept) return undefined;

		const acceptedTypes = accept.split(',').map((a) => a.trim().toLowerCase());
		const fileType = file.type.toLowerCase();
		const fileName = file.name.toLowerCase();

		const isAcceptable = acceptedTypes.some((pattern) => {
			// check extension like .mp4
			if (pattern.startsWith('.')) {
				return fileName.endsWith(pattern);
			}

			// if pattern has wild card like video/*
			if (pattern.endsWith('/*')) {
				const baseType = pattern.slice(0, pattern.indexOf('/*'));
				return fileType.startsWith(baseType + '/');
			}

			// otherwise it must be a specific type like video/mp4
			return fileType === pattern;
		});

		if (!isAcceptable) return 'File type not allowed';

		return undefined;
	};

	const upload = async (uploadFiles: File[]) => {
		if (maxFiles !== undefined && fileCount === undefined) {
			console.warn('Make sure to provide FileDropZone with `fileCount` when using the `maxFiles` prop');
		}
		uploading = true;

		const validFiles: File[] = [];

		for (let i = 0; i < uploadFiles.length; i++) {
			const file = uploadFiles[i];
			if (!file) continue;

			const rejectedReason = shouldAcceptFile(file, (fileCount ?? 0) + i + 1);

			if (rejectedReason) {
				onFileRejected?.({ file, reason: rejectedReason });
				continue;
			}

			validFiles.push(file);
		}

		await onUpload(validFiles);

		uploading = false;
	};

	// Safari's picker greys out files for multi-dot extensions like `.tar.gz`, so the hint uses
	// only the last segment while `shouldAcceptFile` still validates the full pattern.
	const pickerAccept = $derived.by(() => {
		if (!accept) return accept;
		const patterns = accept.split(',').map((pattern) => {
			const trimmed = pattern.trim();
			return trimmed.startsWith('.') ? trimmed.slice(trimmed.lastIndexOf('.')) : trimmed;
		});
		return [...new Set(patterns)].join(',');
	});

	const canUploadFiles = $derived(
		!disabled && !uploading && !(maxFiles !== undefined && fileCount !== undefined && fileCount >= maxFiles)
	);

	const labels = getUiLabels();
</script>

<label
	ondragover={(e) => e.preventDefault()}
	ondrop={drop}
	for={id}
	aria-disabled={!canUploadFiles}
	class={cn(
		'flex h-48 w-full place-items-center justify-center rounded-lg border-2 border-dashed border-border p-6 transition-all hover:cursor-pointer hover:bg-accent/25 aria-disabled:opacity-50 aria-disabled:hover:cursor-not-allowed',
		className
	)}
>
	{#if children}
		{@render children()}
	{:else}
		<div class="flex flex-col place-items-center justify-center gap-2">
			<div
				class="flex size-14 place-items-center justify-center rounded-full border border-dashed border-border text-muted-foreground"
			>
				<UpdateIcon class="size-7" />
			</div>
			<div class="flex flex-col gap-0.5 text-center">
				<span class="font-medium text-muted-foreground">{labels.fileDropZoneDragDrop()}</span>
				{#if maxFiles || maxFileSize}
					<span class="text-sm text-muted-foreground/75">
						{#if maxFiles}
							<span>{labels.fileDropZoneMaxFiles(maxFiles.toString())}</span>
						{/if}
						{#if maxFiles && maxFileSize}
							<span> {labels.fileDropZoneUpToEach(displaySize(maxFileSize))}</span>
						{/if}
						{#if maxFileSize && !maxFiles}
							<span>{labels.fileDropZoneMaxSize(displaySize(maxFileSize))}</span>
						{/if}
					</span>
				{/if}
			</div>
		</div>
	{/if}
	<input
		{...rest}
		disabled={!canUploadFiles}
		{id}
		accept={pickerAccept}
		multiple={multiple ?? (maxFiles === undefined || maxFiles - (fileCount ?? 0) > 1)}
		type="file"
		onchange={change}
		class="hidden"
	/>
</label>
