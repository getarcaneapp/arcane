<script lang="ts">
	import { createQuery, useQueryClient } from '@tanstack/svelte-query';
	import { toast } from 'svelte-sonner';
	import { ResponsiveDialog } from '#lib/components/ui/responsive-dialog/index.js';
	import { Button } from '#lib/components/ui/button/index.js';
	import { Badge } from '#lib/components/ui/badge/index.js';
	import { Checkbox } from '#lib/components/ui/checkbox/index.js';
	import { Input } from '#lib/components/ui/input/index.js';
	import { Label } from '#lib/components/ui/label/index.js';
	import { Progress } from '#lib/components/ui/progress/index.js';
	import * as Alert from '#lib/components/ui/alert/index.js';
	import * as RadioGroup from '#lib/components/ui/radio-group/index.js';
	import * as Select from '#lib/components/ui/select/index.js';
	import * as Table from '#lib/components/ui/table/index.js';
	import Spinner from '#lib/components/ui/spinner/spinner.svelte';
	import { openConfirmDialog } from '#lib/components/confirm-dialog/index.js';
	import { AlertTriangleIcon, SuccessIcon } from '#lib/icons/index.js';
	import { m } from '#lib/paraglide/messages.js';
	import { queryKeys } from '#lib/query/query-keys.js';
	import { APIError } from '#lib/services/api-service.js';
	import { transferService } from '#lib/services/transfer-service.js';
	import { activityStore } from '#lib/stores/activity.store.svelte.js';
	import { environmentStore, LOCAL_DOCKER_ENVIRONMENT_ID } from '#lib/stores/environment.store.svelte.js';
	import type {
		Transfer,
		TransferCleanupResponse,
		TransferKind,
		TransferMode,
		TransferPhase,
		TransferPlan,
		TransferRequest,
		TransferResourceKind,
		TransferResourceProgress,
		TransferResourceStatus,
		TransferStatus
	} from '#lib/types/transfer.type.js';
	import { isTransferSettled } from '#lib/types/transfer.type.js';
	import { extractApiErrorMessage, handleApiResultWithCallbacks } from '#lib/utils/api.js';
	import { bytes } from '#lib/utils/formatting.js';
	import { tryCatch } from '#lib/utils/try-catch.js';

	let {
		open = $bindable(false),
		kind,
		resourceId,
		resourceName,
		onCompleted
	}: {
		open?: boolean;
		kind: TransferKind;
		resourceId: string;
		resourceName: string;
		onCompleted?: () => void | Promise<void>;
	} = $props();

	const POLL_INTERVAL_MS = 2000;

	const queryClient = useQueryClient();
	const sourceEnvId = $derived(environmentStore.selected?.id || LOCAL_DOCKER_ENVIRONMENT_ID);
	const destinationOptions = $derived(environmentStore.available.filter((env) => env.id !== sourceEnvId));
	const resourceLabel = $derived(kind === 'project' ? m.resource_project() : m.resource_volume());

	// Setup form. An empty destination name means "same as the source".
	let step = $state<'setup' | 'review'>('setup');
	let destinationEnvironmentId = $state('');
	let mode = $state<TransferMode>('copy');
	let destinationName = $state('');
	let plan = $state<TransferPlan | null>(null);
	let acknowledged = $state<Record<string, boolean>>({});
	let transferId = $state<string | null>(null);
	let busy = $state(false);

	// Done-phase decisions. Cleanup starts with nothing selected.
	let removeSourceProject = $state(false);
	let removeSourceFiles = $state(false);
	let removeSourceVolumes = $state<Record<string, boolean>>({});
	let cleanupResult = $state<TransferCleanupResponse | null>(null);
	let rollbackAcknowledged = $state(false);

	const effectiveDestinationName = $derived(destinationName.trim() || resourceName);
	const destinationEnvironmentLabel = $derived(
		destinationOptions.find((env) => env.id === destinationEnvironmentId)?.name ||
			m.transfer_destination_environment_placeholder()
	);

	const transferQuery = createQuery(() => ({
		queryKey: queryKeys.transfers.detail(sourceEnvId, transferId ?? ''),
		queryFn: () => transferService.get(kind, transferId ?? ''),
		enabled: !!transferId,
		refetchInterval: (query) => {
			const status = query.state.data?.status;
			if (status && isTransferSettled(status)) return false;
			return POLL_INTERVAL_MS;
		},
		refetchIntervalInBackground: true
	}));
	const transfer = $derived(transferId ? transferQuery.data : undefined);
	const settled = $derived(!!transfer && isTransferSettled(transfer.status));

	const phase = $derived.by(() => {
		if (!transferId) return step;
		if (settled) return 'done';
		return 'running';
	});

	const title = $derived.by(() => {
		if (phase === 'running') return m.transfer_running_title({ name: resourceName });
		if (phase === 'done' && transfer) return statusLabel(transfer.status);
		return m.transfer_dialog_title({ name: resourceName });
	});

	const blocked = $derived((plan?.blockers.length ?? 0) > 0);
	const requiredAcknowledgements = $derived(plan?.requiredAcknowledgements ?? []);
	const allAcknowledged = $derived(requiredAcknowledgements.every((code) => acknowledged[code]));
	const informationalReviews = $derived((plan?.reviews ?? []).filter((item) => !item.required));

	const successfulMove = $derived(transfer?.status === 'succeeded' && transfer.mode === 'move');
	const volumeResources = $derived((transfer?.resources ?? []).filter((resource) => resource.kind === 'volume'));
	const cleanupSelected = $derived(removeSourceProject || removeSourceFiles || Object.values(removeSourceVolumes).some(Boolean));
	const canRollback = $derived(!!transfer && transfer.mode === 'move' && !cleanupResult);

	function statusLabel(status: TransferStatus): string {
		switch (status) {
			case 'succeeded':
				return m.transfer_status_succeeded();
			case 'failed':
				return m.transfer_status_failed();
			case 'canceled':
				return m.transfer_status_canceled();
			case 'needs_attention':
				return m.transfer_status_needs_attention();
			case 'rolled_back':
				return m.transfer_status_rolled_back();
			default:
				return m.common_running();
		}
	}

	function phaseLabel(value: TransferPhase): string {
		switch (value) {
			case 'revalidate':
				return m.transfer_phase_revalidate();
			case 'reserve':
				return m.transfer_phase_reserve();
			case 'prepare':
				return m.transfer_phase_prepare();
			case 'stop':
				return m.transfer_phase_stop();
			case 'copy':
				return m.transfer_phase_copy();
			case 'cutover':
				return m.transfer_phase_cutover();
			case 'recover':
				return m.transfer_phase_recover();
			case 'finished':
				return m.transfer_phase_finished();
			default:
				return m.transfer_phase_pending();
		}
	}

	function resourceKindLabel(value: TransferResourceKind): string {
		switch (value) {
			case 'project_dir':
				return m.transfer_resource_kind_project_dir();
			default:
				return m.transfer_resource_kind_volume();
		}
	}

	function resourceStatusLabel(value: TransferResourceStatus): string {
		switch (value) {
			case 'copying':
				return m.transfer_resource_status_copying();
			case 'verified':
				return m.transfer_resource_status_verified();
			case 'failed':
				return m.common_failed();
			default:
				return m.common_pending();
		}
	}

	function resourceProgress(resource: TransferResourceProgress): { value: number; indeterminate: boolean } {
		if (resource.bytesTotal > 0) {
			return { value: Math.min(100, Math.round((resource.bytesTransferred / resource.bytesTotal) * 100)), indeterminate: false };
		}
		if (resource.status === 'verified') return { value: 100, indeterminate: false };
		if (resource.status === 'pending' || resource.status === 'failed') return { value: 0, indeterminate: false };
		return { value: 100, indeterminate: true };
	}

	function formatSize(value: number): string {
		return bytes.format(value) ?? m.common_unknown();
	}

	// Label for a required acknowledgement: the matching review text, else its code.
	function acknowledgementLabel(code: string): string {
		const matching = (plan?.reviews ?? []).filter((item) => item.code === code).map((item) => item.message);
		return matching.length > 0 ? matching.join(' ') : code;
	}

	function buildRequest(): TransferRequest {
		const request: TransferRequest = {
			kind,
			mode,
			destinationEnvironmentId,
			destinationName: effectiveDestinationName
		};
		if (kind === 'project') {
			request.projectId = resourceId;
		} else {
			request.volumeName = resourceId;
		}
		return request;
	}

	function resetState() {
		step = 'setup';
		destinationEnvironmentId = '';
		mode = 'copy';
		destinationName = '';
		plan = null;
		acknowledged = {};
		transferId = null;
		busy = false;
		removeSourceProject = false;
		removeSourceFiles = false;
		removeSourceVolumes = {};
		cleanupResult = null;
		rollbackAcknowledged = false;
	}

	async function handleReview() {
		busy = true;
		await handleApiResultWithCallbacks({
			result: await tryCatch(transferService.preflight(buildRequest())),
			message: m.transfer_preflight_failed(),
			setLoadingState: (value) => (busy = value),
			onSuccess: (nextPlan) => {
				plan = nextPlan;
				acknowledged = {};
				step = 'review';
			}
		});
	}

	function adoptTransfer(next: Transfer) {
		queryClient.setQueryData(queryKeys.transfers.detail(sourceEnvId, next.id), next);
		transferId = next.id;
	}

	async function handleStart() {
		if (!plan || blocked || !allAcknowledged) return;
		busy = true;
		const result = await tryCatch(
			transferService.create({
				idempotencyKey: crypto.randomUUID(),
				planHash: plan.planHash,
				request: plan.request,
				acknowledgements: requiredAcknowledgements
			})
		);
		// 409: the plan is stale or newly blocked; send the user back to re-plan.
		if (result.error instanceof APIError && result.error.status === 409) {
			busy = false;
			toast.error(m.transfer_plan_changed(), { description: extractApiErrorMessage(result.error) });
			plan = null;
			acknowledged = {};
			step = 'setup';
			return;
		}
		await handleApiResultWithCallbacks({
			result,
			message: m.transfer_start_failed(),
			setLoadingState: (value) => (busy = value),
			onSuccess: adoptTransfer
		});
	}

	async function runTransferAction(action: () => Promise<Transfer>, message: string) {
		busy = true;
		await handleApiResultWithCallbacks({
			result: await tryCatch(action()),
			message,
			setLoadingState: (value) => (busy = value),
			onSuccess: (next) => {
				adoptTransfer(next);
				void transferQuery.refetch();
			}
		});
	}

	function handleCancel() {
		const id = transferId;
		if (!id) return;
		openConfirmDialog({
			title: m.transfer_cancel(),
			message: m.transfer_cancel_confirm_message(),
			confirm: {
				label: m.transfer_cancel(),
				destructive: true,
				action: () => runTransferAction(() => transferService.cancel(kind, id), m.transfer_cancel_failed())
			}
		});
	}

	async function handleRetry() {
		if (!transferId) return;
		const id = transferId;
		cleanupResult = null;
		await runTransferAction(() => transferService.retry(kind, id), m.transfer_retry_failed());
	}

	async function handleRollback() {
		if (!transferId || !rollbackAcknowledged) return;
		const id = transferId;
		await runTransferAction(() => transferService.rollback(kind, id), m.transfer_rollback_failed());
	}

	async function handleReleaseHold() {
		if (!transferId) return;
		const id = transferId;
		await runTransferAction(() => transferService.releaseHold(kind, id), m.transfer_release_hold_failed());
	}

	async function handleCleanup() {
		if (!transferId || !cleanupSelected) return;
		busy = true;
		await handleApiResultWithCallbacks({
			result: await tryCatch(
				transferService.cleanup(kind, transferId, {
					removeSourceProject,
					removeSourceFiles,
					removeSourceVolumes: volumeResources
						.filter((resource) => removeSourceVolumes[resource.key])
						.map((resource) => resource.sourceName)
				})
			),
			message: m.transfer_cleanup_failed(),
			setLoadingState: (value) => (busy = value),
			onSuccess: (result) => {
				cleanupResult = result;
			}
		});
	}

	function openActivity() {
		if (transfer?.activityId) activityStore.openCenter(transfer.activityId, undefined, sourceEnvId);
	}

	async function handleClose() {
		const succeeded = transfer?.status === 'succeeded';
		resetState();
		open = false;
		if (succeeded) await onCompleted?.();
	}
</script>

{#snippet dialogTitle()}
	<span class="inline-flex items-center gap-2">{title}<Badge variant="amber" size="sm">{m.transfer_preview_badge()}</Badge></span>
{/snippet}

<ResponsiveDialog
	bind:open
	title={dialogTitle}
	description={phase === 'setup' ? m.transfer_dialog_description({ resource: resourceLabel }) : undefined}
	contentClass="sm:max-w-150"
	dismissible={phase !== 'running' && !busy}
	onOpenChange={(next) => {
		if (!next) resetState();
	}}
>
	<div class="space-y-5 py-4">
		{#if phase === 'setup'}
			<div class="space-y-2">
				<Label for="transfer-destination-env" class="mb-0">{m.transfer_destination_environment()}</Label>
				<Select.Root type="single" bind:value={destinationEnvironmentId} disabled={busy}>
					<Select.Trigger id="transfer-destination-env" class="w-full">
						<span>{destinationEnvironmentLabel}</span>
					</Select.Trigger>
					<Select.Content>
						{#each destinationOptions as env (env.id)}
							<Select.Item value={env.id} label={env.name}>{env.name}</Select.Item>
						{/each}
					</Select.Content>
				</Select.Root>
			</div>

			<div class="space-y-2">
				<Label class="mb-0">{m.common_mode()}</Label>
				<RadioGroup.Root bind:value={mode} disabled={busy}>
					<label class="flex cursor-pointer items-start gap-3 rounded-lg border p-3">
						<RadioGroup.Item value="copy" class="mt-0.5" />
						<span class="flex flex-col gap-0.5">
							<span class="text-sm font-medium">{m.common_copy()}</span>
							<span class="text-xs text-muted-foreground">{m.transfer_mode_copy_description()}</span>
						</span>
					</label>
					<label class="flex cursor-pointer items-start gap-3 rounded-lg border p-3">
						<RadioGroup.Item value="move" class="mt-0.5" />
						<span class="flex flex-col gap-0.5">
							<span class="text-sm font-medium">{m.transfer_mode_move()}</span>
							<span class="text-xs text-muted-foreground">{m.transfer_mode_move_description()}</span>
						</span>
					</label>
				</RadioGroup.Root>
			</div>

			<div class="space-y-2">
				<Label for="transfer-destination-name" class="mb-0">{m.transfer_destination_name()}</Label>
				<Input id="transfer-destination-name" bind:value={destinationName} placeholder={resourceName} disabled={busy} mono />
			</div>
		{:else if phase === 'review' && plan}
			<div class="flex flex-wrap items-center gap-2 text-sm">
				<span class="font-medium">
					{m.transfer_summary_route({ source: plan.sourceEnvironmentName, destination: plan.destinationEnvironmentName })}
				</span>
				<Badge variant="outline" size="sm">{plan.request.mode === 'move' ? m.transfer_mode_move() : m.common_copy()}</Badge>
				<span class="text-muted-foreground">{m.transfer_estimated_size()}: {formatSize(plan.estimatedBytes)}</span>
			</div>

			{#if blocked}
				<Alert.Root variant="destructive">
					<AlertTriangleIcon class="size-4" />
					<Alert.Title>{m.transfer_blockers_title()}</Alert.Title>
					<Alert.Description>
						<ul class="list-disc space-y-1 pl-4">
							{#each plan.blockers as blocker, index (index)}
								<li>
									{blocker.message}
									{#if blocker.resource}
										<code class="font-mono text-xs">{blocker.resource}</code>
									{/if}
								</li>
							{/each}
						</ul>
					</Alert.Description>
				</Alert.Root>
			{/if}

			{#if plan.requiresDowntime}
				<Alert.Root variant="warning-subtle" size="sm">
					<Alert.Description>{m.transfer_requires_downtime()}</Alert.Description>
				</Alert.Root>
			{/if}
			{#if kind === 'volume' && plan.request.mode === 'move'}
				<Alert.Root variant="info" size="sm">
					<Alert.Description>{m.transfer_volume_move_note()}</Alert.Description>
				</Alert.Root>
			{/if}

			{#if informationalReviews.length > 0}
				<div class="space-y-2">
					<h3 class="text-sm font-semibold">{m.transfer_reviews_title()}</h3>
					<ul class="list-disc space-y-1 pl-4 text-sm text-muted-foreground">
						{#each informationalReviews as item, index (index)}
							<li>
								{item.message}
								{#if item.resource}
									<code class="font-mono text-xs">{item.resource}</code>
								{/if}
							</li>
						{/each}
					</ul>
				</div>
			{/if}

			{#if requiredAcknowledgements.length > 0}
				<div class="space-y-2">
					<h3 class="text-sm font-semibold">{m.transfer_acknowledgements_title()}</h3>
					{#each requiredAcknowledgements as code (code)}
						<label class="flex cursor-pointer items-start gap-3 text-sm">
							<Checkbox
								checked={acknowledged[code] ?? false}
								onCheckedChange={(value) => (acknowledged[code] = value === true)}
								disabled={busy || blocked}
								class="mt-0.5"
							/>
							<span>{acknowledgementLabel(code)}</span>
						</label>
					{/each}
				</div>
			{/if}

			{#if plan.resources.length > 0}
				<div class="space-y-2">
					<h3 class="text-sm font-semibold">{m.transfer_resources_title()}</h3>
					<div class="overflow-hidden rounded-lg border">
						<Table.Root>
							<Table.Header>
								<Table.Row>
									<Table.Head>{m.common_type()}</Table.Head>
									<Table.Head>{m.common_name()}</Table.Head>
									<Table.Head class="text-right">{m.transfer_estimated_size()}</Table.Head>
									<Table.Head class="text-right">{m.transfer_estimated_files()}</Table.Head>
								</Table.Row>
							</Table.Header>
							<Table.Body>
								{#each plan.resources as resource (resource.key)}
									<Table.Row>
										<Table.Cell><span class="text-xs text-muted-foreground">{resourceKindLabel(resource.kind)}</span></Table.Cell>
										<Table.Cell>
											<span class="font-mono text-xs break-all">{resource.sourceName}</span>
											{#if resource.destinationName !== resource.sourceName}
												<span class="font-mono text-xs break-all text-muted-foreground">→ {resource.destinationName}</span>
											{/if}
										</Table.Cell>
										<Table.Cell class="text-right"
											><span class="text-xs tabular-nums">{formatSize(resource.estimatedBytes)}</span></Table.Cell
										>
										<Table.Cell class="text-right"><span class="text-xs tabular-nums">{resource.estimatedFiles}</span></Table.Cell
										>
									</Table.Row>
								{/each}
							</Table.Body>
						</Table.Root>
					</div>
				</div>
			{/if}

			{#if plan.consumers.length > 0}
				<div class="space-y-2">
					<h3 class="text-sm font-semibold">{m.transfer_consumers_title()}</h3>
					<ul class="divide-y rounded-lg border">
						{#each plan.consumers as consumer (consumer.containerId)}
							<li class="flex items-center justify-between gap-3 px-3 py-2 text-sm">
								<span class="truncate font-mono text-xs">{consumer.name}</span>
								<Badge variant={consumer.running ? 'green' : 'gray'} size="sm">
									{consumer.running ? m.common_running() : m.common_stopped()}
								</Badge>
							</li>
						{/each}
					</ul>
				</div>
			{/if}

			{#if kind === 'project' && plan.destinationServices?.length}
				<div class="space-y-2">
					<h3 class="text-sm font-semibold">{m.transfer_destination_services_title()}</h3>
					<div class="flex flex-wrap gap-1.5">
						{#each plan.destinationServices as service (service)}
							<Badge variant="outline" size="sm" mono>{service}</Badge>
						{/each}
					</div>
				</div>
			{/if}
		{:else if transfer}
			<div class="flex flex-wrap items-center gap-2 text-sm">
				{#if phase === 'running'}
					<Spinner class="size-4" />
					<span class="font-medium">{phaseLabel(transfer.phase)}</span>
				{:else if transfer.status === 'succeeded'}
					<SuccessIcon class="size-4 text-success" />
					<span class="font-medium">{phaseLabel(transfer.phase)}</span>
				{:else}
					<AlertTriangleIcon class="size-4 text-destructive" />
					<span class="font-medium">{phaseLabel(transfer.phase)}</span>
				{/if}
				<span class="text-xs text-muted-foreground">{m.transfer_attempt({ attempt: transfer.attempt })}</span>
				{#if transfer.cancelRequested && phase === 'running'}
					<Badge variant="amber" size="sm">{m.transfer_cancel_requested()}</Badge>
				{/if}
				{#if transfer.activityId}
					<Button variant="link" size="inline" class="ml-auto" onclick={openActivity}>{m.activity_view_activity()}</Button>
				{/if}
			</div>

			{#if transfer.status === 'needs_attention'}
				<Alert.Root variant="warning-subtle" size="sm">
					<Alert.Description>{m.transfer_needs_attention_description()}</Alert.Description>
				</Alert.Root>
			{/if}

			{#if transfer.error}
				<Alert.Root variant="destructive">
					<AlertTriangleIcon class="size-4" />
					<Alert.Title>{m.common_error()}</Alert.Title>
					<Alert.Description class="break-words">{transfer.error}</Alert.Description>
				</Alert.Root>
			{/if}

			{#if transfer.resources.length > 0}
				<ul class="space-y-3">
					{#each transfer.resources as resource (resource.key)}
						{@const progress = resourceProgress(resource)}
						<li class="space-y-1.5">
							<div class="flex items-center justify-between gap-3 text-sm">
								<span class="min-w-0 truncate">
									<span class="text-xs text-muted-foreground">{resourceKindLabel(resource.kind)}</span>
									<span class="ml-1 font-mono text-xs">{resource.sourceName}</span>
								</span>
								<span class="shrink-0 text-xs text-muted-foreground tabular-nums">
									{#if resource.bytesTotal > 0}
										{formatSize(resource.bytesTransferred)} / {formatSize(resource.bytesTotal)}
									{:else}
										{resourceStatusLabel(resource.status)}
									{/if}
								</span>
							</div>
							<Progress
								value={progress.value}
								indeterminate={progress.indeterminate}
								tone={resource.status === 'failed' ? 'destructive' : 'default'}
								class="h-1.5"
							/>
							{#if resource.error}
								<p class="text-xs text-destructive">{resource.error}</p>
							{/if}
						</li>
					{/each}
				</ul>
			{/if}

			{#if phase === 'done' && transfer.recovery}
				<div class="space-y-1 rounded-lg border p-3 text-sm">
					<h3 class="font-semibold">{m.transfer_recovery_title()}</h3>
					<p class="text-muted-foreground">
						{transfer.recovery.sourceRestored ? m.transfer_recovery_source_restored() : m.transfer_recovery_source_not_restored()}
					</p>
					<p class="text-muted-foreground">
						{#if transfer.recovery.destinationUnknown}
							{m.transfer_recovery_destination_unknown()}
						{:else if transfer.recovery.destinationRemoved}
							{m.transfer_recovery_destination_removed()}
						{:else}
							{m.transfer_recovery_destination_kept()}
						{/if}
					</p>
					{#if transfer.recovery.incomplete?.length}
						<p class="text-warning">{m.transfer_recovery_incomplete({ items: transfer.recovery.incomplete.join(', ') })}</p>
					{/if}
				</div>
			{/if}

			{#if phase === 'done' && successfulMove}
				<div class="space-y-3 rounded-lg border p-3">
					<div class="space-y-1">
						<h3 class="text-sm font-semibold">{m.transfer_cleanup_title()}</h3>
						<p class="text-xs text-muted-foreground">{m.transfer_cleanup_description()}</p>
					</div>
					{#if cleanupResult}
						<div class="space-y-1 text-xs">
							{#if cleanupResult.removed.length > 0}
								<p class="text-success">{m.transfer_cleanup_removed({ items: cleanupResult.removed.join(', ') })}</p>
							{/if}
							{#each cleanupResult.skipped as skip, index (index)}
								<p class="text-warning">{m.transfer_cleanup_skipped({ resource: skip.resource, reason: skip.reason })}</p>
							{/each}
						</div>
					{:else}
						<div class="space-y-2">
							{#if kind === 'project'}
								<label class="flex cursor-pointer items-center gap-3 text-sm">
									<Checkbox bind:checked={removeSourceProject} disabled={busy} />
									<span>{m.transfer_cleanup_remove_project()}</span>
								</label>
								<label class="flex cursor-pointer items-center gap-3 text-sm">
									<Checkbox bind:checked={removeSourceFiles} disabled={busy} />
									<span>{m.transfer_cleanup_remove_files()}</span>
								</label>
							{/if}
							{#each volumeResources as resource (resource.key)}
								<label class="flex cursor-pointer items-center gap-3 text-sm">
									<Checkbox
										checked={removeSourceVolumes[resource.key] ?? false}
										onCheckedChange={(value) => (removeSourceVolumes[resource.key] = value === true)}
										disabled={busy}
									/>
									<span class="break-all">{m.transfer_cleanup_remove_volume({ name: resource.sourceName })}</span>
								</label>
							{/each}
						</div>
						<Button variant="destructive" size="sm" disabled={busy || !cleanupSelected} onclick={handleCleanup}>
							{m.transfer_cleanup_run()}
						</Button>
					{/if}
				</div>
			{/if}

			{#if phase === 'done' && canRollback && (transfer.status === 'succeeded' || transfer.status === 'needs_attention')}
				<div class="space-y-3 rounded-lg border p-3">
					<div class="space-y-1">
						<h3 class="text-sm font-semibold">{m.transfer_rollback_title()}</h3>
						<p class="text-xs text-muted-foreground">{m.transfer_rollback_description()}</p>
					</div>
					<label class="flex cursor-pointer items-start gap-3 text-sm">
						<Checkbox bind:checked={rollbackAcknowledged} disabled={busy} class="mt-0.5" />
						<span>{m.transfer_rollback_acknowledge()}</span>
					</label>
					<div class="flex flex-wrap gap-2">
						<Button variant="destructive" size="sm" disabled={busy || !rollbackAcknowledged} onclick={handleRollback}>
							{m.transfer_rollback_title()}
						</Button>
						{#if transfer.status === 'needs_attention'}
							<Button variant="outline" size="sm" disabled={busy} onclick={handleRetry}>{m.common_retry()}</Button>
						{/if}
					</div>
				</div>
			{/if}

			{#if phase === 'done' && transfer.sourceHeld}
				<div class="flex flex-wrap items-center justify-between gap-3 rounded-lg border p-3">
					<div class="space-y-1">
						<h3 class="text-sm font-semibold">{m.transfer_release_hold()}</h3>
						<p class="text-xs text-muted-foreground">{m.transfer_release_hold_description()}</p>
					</div>
					<Button variant="outline" size="sm" disabled={busy} onclick={handleReleaseHold}>{m.transfer_release_hold()}</Button>
				</div>
			{/if}
		{:else}
			<div class="flex items-center gap-2 text-sm text-muted-foreground">
				<Spinner class="size-4" />
				<span>{m.transfer_phase_pending()}</span>
			</div>
		{/if}
	</div>

	{#snippet footer()}
		{#if phase === 'setup'}
			<Button variant="outline" disabled={busy} onclick={() => (open = false)}>{m.common_cancel()}</Button>
			<Button disabled={busy || !destinationEnvironmentId} onclick={handleReview}>
				{#if busy}
					<Spinner class="size-4" />
				{/if}
				{m.transfer_review()}
			</Button>
		{:else if phase === 'review'}
			<Button variant="outline" disabled={busy} onclick={() => (step = 'setup')}>{m.common_back()}</Button>
			<Button disabled={busy || blocked || !allAcknowledged} onclick={handleStart}>
				{#if busy}
					<Spinner class="size-4" />
				{/if}
				{m.transfer_start()}
			</Button>
		{:else if phase === 'running'}
			<Button variant="destructive" disabled={busy || !!transfer?.cancelRequested} onclick={handleCancel}
				>{m.transfer_cancel()}</Button
			>
		{:else}
			<Button variant="outline" disabled={busy} onclick={handleClose}>{m.common_done()}</Button>
		{/if}
	{/snippet}
</ResponsiveDialog>
