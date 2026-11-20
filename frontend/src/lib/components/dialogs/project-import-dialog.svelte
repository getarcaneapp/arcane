<script lang="ts">
	import { toast } from 'svelte-sonner';

	import { ArcaneButton } from '#lib/components/arcane-button/index.js';
	import EnvironmentDestinationSelect from '#lib/components/form/environment-destination-select.svelte';
	import LabeledSwitch from '#lib/components/form/labeled-switch.svelte';
	import { displaySize, FileDropZone, type FileDropZoneProps } from '#lib/components/ui/file-drop-zone/index.js';
	import { Input } from '#lib/components/ui/input/index.js';
	import { Label } from '#lib/components/ui/label/index.js';
	import { Progress } from '#lib/components/ui/progress/index.js';
	import { ResponsiveDialog } from '#lib/components/ui/responsive-dialog/index.js';
	import { Spinner } from '#lib/components/ui/spinner/index.js';
	import { CloseIcon, UploadIcon } from '#lib/icons/index.js';
	import { m } from '#lib/paraglide/messages.js';
	import { projectService } from '#lib/services/project-service.js';
	import type { ChunkedUploadProgress } from '#lib/types/ui/upload.js';
	import { handleApiResultWithCallbacks } from '#lib/utils/api.js';
	import { hasPermission } from '#lib/utils/auth.js';
	import { destinationEnvironmentId } from '#lib/utils/environments.js';
	import { tryCatch } from '#lib/utils/try-catch.js';

	let {
		open = $bindable(false),
		onImported
	}: {
		open?: boolean;
		onImported: (environmentId: string) => Promise<void>;
	} = $props();

	const archiveSuffixes = ['.arcane.tar.gz', '.tar.gz', '.tgz'];

	let file = $state<File | null>(null);
	let nameOverride = $state<string | null>(null);
	let start = $state(false);
	let importing = $state(false);
	let progress = $state<ChunkedUploadProgress | null>(null);
	let pickedEnvironmentId = $state('');
	const environmentId = $derived(destinationEnvironmentId(pickedEnvironmentId, 'projects:create'));
	const canStart = $derived(hasPermission('projects:deploy', environmentId));
	const name = $derived(nameOverride ?? archiveName(file?.name ?? ''));
	const percent = $derived(
		progress && progress.totalBytes > 0 ? Math.round((progress.bytesDone / progress.totalBytes) * 100) : 0
	);

	function archiveName(filename: string): string {
		const suffix = archiveSuffixes.find((candidate) => filename.toLowerCase().endsWith(candidate));
		if (!suffix) return filename;
		return filename.slice(0, -suffix.length);
	}

	const onFile: FileDropZoneProps['onUpload'] = async (files) => {
		file = files[0] ?? file;
	};

	const onFileRejected: FileDropZoneProps['onFileRejected'] = async ({ reason, file: rejected }) => {
		toast.error(m.project_import_failed(), { description: `${rejected.name}: ${reason}` });
	};

	function reset() {
		file = null;
		nameOverride = null;
		start = false;
		progress = null;
		pickedEnvironmentId = '';
		open = false;
	}

	function handleOpenChange(next: boolean) {
		if (!next && !importing) reset();
	}

	async function handleImport() {
		if (!file || !name.trim() || importing) return;
		const requestedEnvironmentId = environmentId;
		importing = true;
		const result = await tryCatch(
			projectService.importProject(
				file,
				{ name: name.trim(), start: start && canStart },
				requestedEnvironmentId,
				(next) => (progress = next)
			)
		);
		await handleApiResultWithCallbacks({
			result,
			message: m.project_import_failed(),
			setLoadingState: (value) => (importing = value),
			onSuccess: async (project) => {
				toast.success(m.project_imported({ name: project.name }));
				reset();
				await onImported(requestedEnvironmentId);
			},
			onError: () => {
				progress = null;
			}
		});
	}
</script>

<ResponsiveDialog
	{open}
	onOpenChange={handleOpenChange}
	title={m.import_project()}
	description={m.import_project_description()}
	contentClass="sm:max-w-xl"
	dismissible={!importing}
>
	<div class="grid min-w-0 gap-y-4 py-4" data-testid="project-import-dialog">
		<EnvironmentDestinationSelect
			bind:value={() => environmentId, (value) => (pickedEnvironmentId = value)}
			permission="projects:create"
			variant="field"
		/>
		{#if file}
			<div class="flex min-w-0 items-center justify-between gap-2 rounded-lg border border-border bg-muted/50 p-3">
				<div class="flex min-w-0 flex-col">
					<span class="text-sm font-medium break-all">{file.name}</span>
					<span class="text-xs text-muted-foreground">{displaySize(file.size)}</span>
				</div>
				<ArcaneButton
					action="base"
					tone="ghost"
					size="icon"
					onclick={() => (file = null)}
					disabled={importing}
					icon={CloseIcon}
				/>
			</div>
		{:else}
			<FileDropZone onUpload={onFile} {onFileRejected} accept=".tar.gz,.tgz" maxFiles={1} fileCount={0} disabled={importing} />
		{/if}
		<div class="space-y-2">
			<Label for="import-project-name">{m.project_name()}</Label>
			<Input
				id="import-project-name"
				value={name}
				autocomplete="off"
				disabled={importing}
				oninput={(event) => (nameOverride = event.currentTarget.value)}
			/>
		</div>
		<LabeledSwitch
			id="import-start"
			checked={start && canStart}
			label={m.start_after_import()}
			disabled={!canStart || importing}
			onCheckedChange={(next) => (start = next)}
		/>
		{#if importing}
			<div class="flex flex-col gap-2" aria-live="polite">
				<div class="flex items-center gap-2 text-sm text-muted-foreground">
					<Spinner class="size-4" />
					<span>{progress && percent < 100 ? m.uploading() : m.importing_project()}</span>
					{#if progress && percent < 100}
						<span class="ml-auto tabular-nums">{percent}%</span>
					{/if}
				</div>
				{#if progress && percent < 100}
					<Progress value={percent} class="h-1.5" />
				{/if}
			</div>
		{/if}
	</div>

	{#snippet footer()}
		<ArcaneButton action="cancel" onclick={reset} disabled={importing} />
		<ArcaneButton
			action="create"
			customLabel={m.import()}
			icon={UploadIcon}
			onclick={handleImport}
			loading={importing}
			disabled={!file || !name.trim() || importing}
		/>
	{/snippet}
</ResponsiveDialog>
