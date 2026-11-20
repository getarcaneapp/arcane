<script lang="ts">
	import { createQuery } from '@tanstack/svelte-query';
	import { toast } from 'svelte-sonner';

	import { ArcaneButton } from '#lib/components/arcane-button/index.js';
	import LabeledSwitch from '#lib/components/form/labeled-switch.svelte';
	import * as Alert from '#lib/components/ui/alert/index.js';
	import { Checkbox } from '#lib/components/ui/checkbox/index.js';
	import { Label } from '#lib/components/ui/label/index.js';
	import { ResponsiveDialog } from '#lib/components/ui/responsive-dialog/index.js';
	import { Spinner } from '#lib/components/ui/spinner/index.js';
	import { AlertIcon, DownloadIcon } from '#lib/icons/index.js';
	import { m } from '#lib/paraglide/messages.js';
	import { projectExportOptionsQuery } from '#lib/query/projects.js';
	import { projectService } from '#lib/services/project-service.js';
	import type { Project, ProjectExportOptions, ProjectVolume } from '#lib/types/project.js';
	import { extractApiErrorMessage, handleApiResultWithCallbacks } from '#lib/utils/api.js';
	import { hasPermission } from '#lib/utils/auth.js';
	import { tryCatch } from '#lib/utils/try-catch.js';

	let {
		open = $bindable(false),
		project,
		environmentId
	}: {
		open?: boolean;
		project: Pick<Project, 'id' | 'name'> | null;
		environmentId: string;
	} = $props();

	const optionsQuery = createQuery(() => ({
		...projectExportOptionsQuery(environmentId, project?.id ?? ''),
		enabled: open && !!project
	}));
	const options = $derived<ProjectExportOptions | null>(optionsQuery.isFetching ? null : (optionsQuery.data ?? null));
	const optionsError = $derived(
		optionsQuery.error && !optionsQuery.isFetching ? extractApiErrorMessage(optionsQuery.error) : null
	);
	let checkedOverrides = $state<Record<string, boolean>>({});
	let stopOverride = $state<boolean | null>(null);
	let exporting = $state(false);

	const canBackupVolumes = $derived(hasPermission('volumes:backup', environmentId));
	const canStop = $derived(hasPermission('projects:down', environmentId) && hasPermission('projects:deploy', environmentId));
	const selectable = (volume: ProjectVolume) => canBackupVolumes && volume.included;
	const selectedVolumes = $derived(
		(options?.volumes ?? []).filter((volume) => selectable(volume) && (checkedOverrides[volume.key] ?? volume.included))
	);
	const showStop = $derived(!!options?.running && selectedVolumes.length > 0);
	const stopProject = $derived(showStop && canStop && (stopOverride ?? true));

	function volumeNote(volume: ProjectVolume): string | null {
		if (volume.backupStore) return m.backup_store_not_exported();
		if (volume.missing) return m.volume_not_created_yet();
		if (volume.kind === 'driverOpts') return m.volume_kind_driver_opts();
		if (volume.kind === 'external' && !volume.included) return m.volume_kind_external();
		if (volume.inUse) return m.volume_in_use_elsewhere();
		return null;
	}

	function handleOpenChange(next: boolean) {
		if (!next) handleClose();
	}

	function handleClose() {
		if (!exporting) reset();
	}

	function reset() {
		checkedOverrides = {};
		stopOverride = null;
		open = false;
	}

	async function handleExport() {
		if (!project || !options || optionsError || exporting) return;
		exporting = true;
		const result = await tryCatch(
			projectService.exportProject(
				project.id,
				{ volumes: selectedVolumes.map((volume) => volume.key), stopProject },
				environmentId
			)
		);
		await handleApiResultWithCallbacks({
			result,
			message: m.project_export_failed(),
			setLoadingState: (value) => (exporting = value),
			onSuccess: () => {
				toast.success(m.project_exported({ name: project?.name ?? '' }));
				reset();
			}
		});
	}
</script>

<ResponsiveDialog
	{open}
	onOpenChange={handleOpenChange}
	title={m.export_project()}
	description={m.export_project_description()}
	contentClass="sm:max-w-xl"
	dismissible={!exporting}
>
	<div class="grid min-w-0 gap-y-4 py-4" data-testid="project-export-dialog">
		{#if optionsError}
			<Alert.Root variant="destructive">
				<AlertIcon class="size-4" />
				<Alert.Description>{optionsError}</Alert.Description>
			</Alert.Root>
		{:else if !options}
			<div class="flex items-center gap-2 text-sm text-muted-foreground">
				<Spinner class="size-4" />
				<span>{m.common_loading()}</span>
			</div>
		{:else if options.volumes.length === 0}
			<p class="text-sm text-muted-foreground">{m.export_no_volumes()}</p>
		{:else}
			<div class="min-w-0 space-y-2">
				<p class="text-sm font-medium">{m.include_volume_data()}</p>
				{#if !canBackupVolumes}
					<p class="text-xs text-muted-foreground">{m.export_volumes_permission_required()}</p>
				{/if}
				<ul class="space-y-2">
					{#each options.volumes as volume (volume.key)}
						{@const note = volumeNote(volume)}
						<li class="flex min-w-0 items-start gap-3 rounded-xl border bg-card/10 p-3">
							<Checkbox
								id={`export-volume-${volume.key}`}
								class="mt-0.5"
								checked={selectable(volume) && (checkedOverrides[volume.key] ?? volume.included)}
								disabled={!selectable(volume) || exporting}
								onCheckedChange={(checked) => (checkedOverrides = { ...checkedOverrides, [volume.key]: checked === true })}
							/>
							<div class="grid min-w-0 gap-1">
								<Label for={`export-volume-${volume.key}`} class="mb-0 break-all">{volume.key}</Label>
								<span class="font-mono text-xs break-all text-muted-foreground">{volume.name}</span>
								{#if note}
									<span class={['text-xs', volume.inUse ? 'text-warning' : 'text-muted-foreground']}>{note}</span>
								{/if}
							</div>
						</li>
					{/each}
				</ul>
			</div>
			{#if showStop}
				<LabeledSwitch
					id="export-stop-project"
					checked={stopProject}
					label={m.stop_project_during_export()}
					description={canStop ? m.stop_project_during_export_description() : m.stop_project_permission_required()}
					disabled={!canStop || exporting}
					onCheckedChange={(next) => (stopOverride = next)}
				/>
			{/if}
		{/if}

		{#if exporting}
			<div class="flex items-center gap-2 text-sm text-muted-foreground" aria-live="polite">
				<Spinner class="size-4" />
				<span>{m.vuln_exporting()}</span>
			</div>
		{/if}
	</div>

	{#snippet footer()}
		<ArcaneButton action="cancel" onclick={handleClose} disabled={exporting} />
		<ArcaneButton
			action="base"
			customLabel={m.images_export()}
			icon={DownloadIcon}
			onclick={handleExport}
			loading={exporting}
			disabled={!options || !!optionsError || exporting}
		/>
	{/snippet}
</ResponsiveDialog>
