<script lang="ts" module>
	import { environmentStore } from '#lib/stores/environment.store.svelte.js';
	import { hasPermission } from '#lib/utils/auth.js';
	import { isEnvironmentOnline } from '#lib/utils/docker.js';

	export function projectTransferTargets(sourceEnvironmentId: string) {
		return environmentStore.available.filter(
			(env) =>
				env.enabled &&
				env.id !== sourceEnvironmentId &&
				isEnvironmentOnline(env) &&
				['projects:create', 'projects:read', 'volumes:read', 'networks:read', 'containers:list'].every((perm) =>
					hasPermission(perm, env.id)
				)
		);
	}
</script>

<script lang="ts">
	import * as Alert from '@arcane/ui/alert';
	import { Input } from '@arcane/ui/input';
	import { Label } from '@arcane/ui/label';
	import { Spinner } from '@arcane/ui/spinner';
	import * as Tabs from '@arcane/ui/tabs';
	import { toast } from 'svelte-sonner';

	import { ArcaneButton } from '#lib/components/arcane-button/index.js';
	import LabeledSwitch from '#lib/components/form/labeled-switch.svelte';
	import SelectWithLabel from '#lib/components/form/select-with-label.svelte';
	import { ResponsiveDialog } from '#lib/components/responsive-dialog/index.js';
	import { AlertIcon, CopyIcon, FolderMoveIcon } from '#lib/icons/index.js';
	import { m } from '#lib/paraglide/messages.js';
	import { projectService } from '#lib/services/project-service.js';
	import { activityStore } from '#lib/stores/activity.store.svelte.js';
	import type {
		Project,
		ProjectTransferCheck,
		ProjectTransferMode,
		ProjectTransferPreflight,
		ProjectTransferRequest,
		ProjectTransferResource
	} from '#lib/types/project.js';
	import { extractApiErrorMessage, handleApiResultWithCallbacks } from '#lib/utils/api.js';
	import { tryCatch } from '#lib/utils/try-catch.js';

	let {
		open = $bindable(false),
		project,
		environmentId
	}: {
		open?: boolean;
		project: Pick<Project, 'id' | 'name' | 'status'> | null;
		environmentId: string;
	} = $props();

	let mode = $state<ProjectTransferMode>('copy');
	let targetEnvironmentId = $state('');
	let nameOverride = $state<string | null>(null);
	let includeVolumesOverride = $state<boolean | null>(null);
	let startOverride = $state<boolean | null>(null);
	let preflight = $state<ProjectTransferPreflight | null>(null);
	let preflightKey = $state('');
	let preflightLoading = $state(false);
	let preflightError = $state<string | null>(null);
	let submitting = $state(false);
	let creatingResource = $state<string | null>(null);
	let preflightSeq = 0;
	let createSeq = 0;

	const missingResourceKinds: Record<string, ProjectTransferResource['kind']> = {
		external_volume_missing: 'volume',
		external_network_missing: 'network'
	};

	const sourceRunning = $derived(project?.status === 'running' || project?.status === 'partially running');
	const name = $derived(nameOverride ?? project?.name ?? '');
	const includeVolumes = $derived(includeVolumesOverride ?? mode === 'move');
	const start = $derived(startOverride ?? sourceRunning);
	const targets = $derived(projectTransferTargets(environmentId));
	const targetOptions = $derived(targets.map((env) => ({ value: env.id, label: env.name })));
	const targetName = $derived(targets.find((env) => env.id === targetEnvironmentId)?.name ?? targetEnvironmentId);
	const request = $derived<ProjectTransferRequest>({
		targetEnvironmentId,
		mode,
		name: name.trim(),
		includeVolumes,
		start
	});
	const requestKey = $derived(JSON.stringify(request));
	const sortedChecks = $derived(
		[...(preflight?.checks ?? [])].sort((a, b) => Number(a.severity !== 'error') - Number(b.severity !== 'error'))
	);
	const canSubmit = $derived(!!preflight && preflight.allowed && preflightKey === requestKey && !preflightLoading && !submitting);

	async function runPreflight() {
		const seq = ++preflightSeq;
		if (!project || !request.targetEnvironmentId || !request.name) {
			preflight = null;
			preflightError = null;
			preflightLoading = false;
			return;
		}
		const key = requestKey;
		preflightLoading = true;
		preflightError = null;
		const result = await tryCatch(projectService.preflightTransfer(project.id, request, environmentId));
		if (seq !== preflightSeq) return;
		preflightLoading = false;
		if (result.error) {
			preflight = null;
			preflightError = extractApiErrorMessage(result.error);
			return;
		}
		preflight = result.data;
		preflightKey = key;
	}

	function missingResource(check: ProjectTransferCheck): ProjectTransferResource | null {
		const kind: ProjectTransferResource['kind'] | undefined = missingResourceKinds[check.code];
		if (!kind || !check.resource || !hasPermission(`${kind}s:create`, targetEnvironmentId)) {
			return null;
		}
		return { targetEnvironmentId, kind, name: check.resource };
	}

	async function handleCreateResource(resource: ProjectTransferResource) {
		if (!project) return;
		const seq = ++createSeq;
		creatingResource = resource.name;
		const result = await tryCatch(projectService.createTransferResource(project.id, resource, environmentId));
		await handleApiResultWithCallbacks({
			result,
			message: m.common_create_failed({ resource: resource.name }),
			setLoadingState: (value) => {
				if (seq === createSeq) creatingResource = value ? resource.name : null;
			},
			onSuccess: () => {
				toast.success(m.resource_created_on_target({ name: resource.name }));
				if (seq === createSeq) void runPreflight();
			}
		});
	}

	function handleModeChange(next: string) {
		mode = next === 'move' ? 'move' : 'copy';
		includeVolumesOverride = null;
		void runPreflight();
	}

	function handleTargetChange(next: string) {
		targetEnvironmentId = next;
		void runPreflight();
	}

	function handleIncludeVolumesChange(next: boolean) {
		includeVolumesOverride = next;
		void runPreflight();
	}

	function handleStartChange(next: boolean) {
		startOverride = next;
		void runPreflight();
	}

	function handleOpenChange(next: boolean) {
		if (!next) handleClose();
	}

	function handleClose() {
		preflightSeq++;
		createSeq++;
		mode = 'copy';
		targetEnvironmentId = '';
		nameOverride = null;
		includeVolumesOverride = null;
		startOverride = null;
		preflight = null;
		preflightKey = '';
		preflightLoading = false;
		preflightError = null;
		submitting = false;
		creatingResource = null;
		open = false;
	}

	async function handleSubmit() {
		if (!project || !canSubmit) return;
		const startedMessage = mode === 'move' ? m.move_started() : m.copy_started();
		submitting = true;
		const result = await tryCatch(projectService.startTransfer(project.id, request, environmentId));
		await handleApiResultWithCallbacks({
			result,
			message: m.copy_or_move_failed(),
			setLoadingState: (value) => (submitting = value),
			onSuccess: (data) => {
				handleClose();
				toast.success(startedMessage);
				if (hasPermission('activities:read', environmentId)) {
					activityStore.openCenter(data.activityId, undefined, environmentId);
				}
			}
		});
	}
</script>

<ResponsiveDialog
	{open}
	onOpenChange={handleOpenChange}
	title={m.copy_or_move_project()}
	description={m.copy_or_move_project_description()}
	contentClass="sm:max-w-xl"
>
	<div class="grid gap-y-4 py-4">
		<div class="space-y-2">
			<Label>{m.common_mode()}</Label>
			<Tabs.Root value={mode} onValueChange={handleModeChange}>
				<Tabs.List aria-label={m.common_mode()} class="min-h-8 w-full sm:w-fit">
					<Tabs.Trigger value="copy" size="sm" class="h-7 flex-1 sm:flex-none">{m.common_copy()}</Tabs.Trigger>
					<Tabs.Trigger value="move" size="sm" class="h-7 flex-1 sm:flex-none">{m.move()}</Tabs.Trigger>
				</Tabs.List>
			</Tabs.Root>
		</div>

		<SelectWithLabel
			id="transfer-target-environment"
			label={m.target_environment()}
			value={targetEnvironmentId}
			options={targetOptions}
			onValueChange={handleTargetChange}
		/>

		<div class="space-y-2">
			<Label for="transfer-project-name">{m.project_name()}</Label>
			<Input
				id="transfer-project-name"
				value={name}
				autocomplete="off"
				oninput={(event) => (nameOverride = event.currentTarget.value)}
				onchange={() => void runPreflight()}
			/>
		</div>

		<LabeledSwitch
			id="transfer-include-volumes"
			checked={includeVolumes}
			label={m.include_volume_data()}
			description={m.include_volume_data_description()}
			onCheckedChange={handleIncludeVolumesChange}
		/>

		<LabeledSwitch id="transfer-start" checked={start} label={m.start_on_target()} onCheckedChange={handleStartChange} />

		{#if preflightLoading}
			<div class="flex items-center gap-2 text-sm text-muted-foreground">
				<Spinner class="size-4" />
				<span>{m.common_loading()}</span>
			</div>
		{:else if preflightError}
			<Alert.Root variant="destructive">
				<AlertIcon class="size-4" />
				<Alert.Description>{preflightError}</Alert.Description>
			</Alert.Root>
		{:else if preflight && preflightKey === requestKey}
			<div class="space-y-3 rounded-xl border bg-card/10 p-4 text-sm">
				<p>
					{(mode === 'move' ? m.move_project_summary : m.copy_project_summary)({
						project: preflight.projectName,
						environment: targetName,
						name: request.name
					})}
				</p>
				{#if includeVolumes && preflight.volumes.length > 0}
					<div class="space-y-1">
						<p class="text-xs text-muted-foreground">{m.resource_volumes_cap()}</p>
						<ul class="space-y-1 font-mono text-xs">
							{#each preflight.volumes as volume (volume.key)}
								<li class="break-all">{volume.sourceName} → {volume.targetName}</li>
							{/each}
						</ul>
					</div>
				{:else if !includeVolumes && preflight.volumes.length > 0}
					<p class="text-muted-foreground">{m.volume_data_not_copied()}</p>
				{/if}
				{#if mode === 'move'}
					<p class="text-muted-foreground">{m.move_source_kept()}</p>
				{/if}
			</div>
			{#each sortedChecks as check (check.code + check.message)}
				{@const resource = missingResource(check)}
				<Alert.Root variant={check.severity === 'error' ? 'destructive' : 'warning'} size="sm">
					<AlertIcon class="size-4" />
					<div class="flex items-center justify-between gap-3">
						<Alert.Description>{check.message}</Alert.Description>
						{#if resource}
							<ArcaneButton
								action="create"
								class="shrink-0"
								onclick={() => handleCreateResource(resource)}
								loading={creatingResource === resource.name}
								disabled={creatingResource !== null}
							/>
						{/if}
					</div>
				</Alert.Root>
			{/each}
		{/if}
	</div>

	{#snippet footer()}
		<ArcaneButton action="cancel" onclick={handleClose} />
		<ArcaneButton
			action="confirm"
			customLabel={mode === 'move' ? m.move() : m.common_copy()}
			icon={mode === 'move' ? FolderMoveIcon : CopyIcon}
			onclick={handleSubmit}
			loading={submitting}
			disabled={!canSubmit}
		/>
	{/snippet}
</ResponsiveDialog>
