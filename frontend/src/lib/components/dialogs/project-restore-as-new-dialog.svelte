<script lang="ts">
	import { goto } from '$app/navigation';
	import { resolve } from '$app/paths';
	import { toast } from 'svelte-sonner';

	import { ArcaneButton } from '#lib/components/arcane-button/index.js';
	import LabeledSwitch from '#lib/components/form/labeled-switch.svelte';
	import ProjectVolumeChecklist from '#lib/components/project-volume-checklist.svelte';
	import { Checkbox } from '#lib/components/ui/checkbox/index.js';
	import { Input } from '#lib/components/ui/input/index.js';
	import { Label } from '#lib/components/ui/label/index.js';
	import { ResponsiveDialog } from '#lib/components/ui/responsive-dialog/index.js';
	import { m } from '#lib/paraglide/messages.js';
	import { projectService } from '#lib/services/project-service.js';
	import type { ProjectBackup, ProjectVolume } from '#lib/types/project.js';
	import { activityToastOptions } from '#lib/utils/activity-toast.js';
	import { handleApiResultWithCallbacks } from '#lib/utils/api.js';
	import { hasPermission } from '#lib/utils/auth.js';
	import { restorableVolumes } from '#lib/utils/backups.js';
	import { tryCatch } from '#lib/utils/try-catch.js';

	let {
		open = $bindable(false),
		backup,
		environmentId
	}: {
		open?: boolean;
		backup: ProjectBackup | null;
		environmentId: string;
	} = $props();

	let nameOverride = $state<string | null>(null);
	let overrides = $state<Record<string, boolean>>({});
	let start = $state(false);
	let shareExternalVolumes = $state(false);
	let restoring = $state(false);

	const canStart = $derived(hasPermission('projects:deploy', environmentId));
	const name = $derived(nameOverride ?? (backup ? `${backup.projectName}-restored` : ''));
	const volumes = $derived(restorableVolumes(backup?.volumes ?? []));
	// A project restored as new keeps an external volume's name, so the volume is restored only when chosen.
	const restoreByDefault = (volume: ProjectVolume) => volume.kind !== 'external';
	const selectedVolumes = $derived(volumes.filter((volume) => overrides[volume.key] ?? restoreByDefault(volume)));
	const sharesExternalVolumes = $derived(
		(backup?.volumes ?? []).some(
			(volume) => volume.kind === 'external' && !selectedVolumes.some((selected) => selected.key === volume.key)
		)
	);

	function handleOpenChange(next: boolean) {
		if (!next) handleClose();
	}

	function handleClose() {
		if (!restoring) reset();
	}

	function reset() {
		nameOverride = null;
		overrides = {};
		start = false;
		shareExternalVolumes = false;
		open = false;
	}

	async function handleRestore() {
		if (!backup || restoring || !name.trim()) return;
		restoring = true;
		await handleApiResultWithCallbacks({
			result: await tryCatch(
				projectService.restoreBackupAsNew(
					backup.projectId,
					backup.id,
					{
						name: name.trim(),
						volumes: selectedVolumes.map((volume) => volume.key),
						start: start && canStart,
						shareExternalVolumes: sharesExternalVolumes && shareExternalVolumes
					},
					environmentId
				)
			),
			message: m.failed_to_restore_backup(),
			setLoadingState: (value) => (restoring = value),
			onSuccess: async (project) => {
				reset();
				toast.success(m.project_restored_from_backup({ name: project.name }), activityToastOptions(project.activityId));
				await goto(resolve(`/(app)/projects/[projectId]?env=${encodeURIComponent(environmentId)}`, { projectId: project.id }));
			}
		});
	}
</script>

<ResponsiveDialog
	{open}
	onOpenChange={handleOpenChange}
	title={m.restore_as_new_project()}
	description={m.restore_as_new_project_description()}
	contentClass="sm:max-w-xl"
	dismissible={!restoring}
>
	<div class="grid min-w-0 gap-y-4 py-4" data-testid="project-restore-as-new-dialog">
		<div class="space-y-2">
			<Label for="project-restore-as-new-name">{m.project_name()}</Label>
			<Input
				id="project-restore-as-new-name"
				value={name}
				autocomplete="off"
				disabled={restoring}
				oninput={(event) => (nameOverride = event.currentTarget.value)}
			/>
		</div>
		<div class="min-w-0 space-y-2">
			<p class="text-sm font-medium">{m.what_to_restore()}</p>
			<div class="flex min-w-0 items-start gap-3 rounded-xl border bg-card/10 p-3">
				<Checkbox id="project-restore-as-new-files" class="mt-0.5" checked disabled />
				<Label for="project-restore-as-new-files" class="mb-0">{m.project_files()}</Label>
			</div>
			<ProjectVolumeChecklist
				{volumes}
				bind:overrides
				selectable={() => true}
				checkedByDefault={restoreByDefault}
				note={(volume) => (volume.kind === 'external' ? m.external_volume_keeps_its_name() : null)}
				idPrefix="project-restore-as-new-volume"
				disabled={restoring}
			/>
		</div>
		{#if sharesExternalVolumes}
			<LabeledSwitch
				id="project-restore-as-new-share-external"
				checked={shareExternalVolumes}
				label={m.share_external_volumes()}
				description={m.share_external_volumes_description()}
				disabled={restoring}
				onCheckedChange={(next) => (shareExternalVolumes = next)}
			/>
		{/if}
		<LabeledSwitch
			id="project-restore-as-new-start"
			checked={start && canStart}
			label={m.start_after_restore()}
			disabled={!canStart || restoring}
			onCheckedChange={(next) => (start = next)}
		/>
	</div>

	{#snippet footer()}
		<ArcaneButton action="cancel" onclick={handleClose} disabled={restoring} />
		<ArcaneButton
			action="create"
			customLabel={m.restore_as_new_project()}
			onclick={handleRestore}
			loading={restoring}
			disabled={restoring || !name.trim() || (sharesExternalVolumes && !shareExternalVolumes)}
			data-testid="project-restore-as-new-submit"
		/>
	{/snippet}
</ResponsiveDialog>
