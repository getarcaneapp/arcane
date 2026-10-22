<script lang="ts">
	import { toast } from 'svelte-sonner';

	import { ArcaneButton } from '#lib/components/arcane-button/index.js';
	import EnvironmentDestinationSelect from '#lib/components/form/environment-destination-select.svelte';
	import LabeledSwitch from '#lib/components/form/labeled-switch.svelte';
	import { DetailSection, KeyValueCard, KeyValueGrid } from '#lib/components/resource-detail/index.js';
	import EmptyState from '#lib/components/states/empty-state.svelte';
	import { Badge } from '#lib/components/ui/badge/index.js';
	import * as Table from '#lib/components/ui/table/index.js';
	import { AlertIcon, InfoIcon, UpdateIcon } from '#lib/icons/index.js';
	import { ResourcePageLayout } from '#lib/layouts/index.js';
	import { m } from '#lib/paraglide/messages.js';
	import { PERMISSIONS } from '#lib/permissions.generated.js';
	import { podmanService } from '#lib/services/podman-service.js';
	import type { PodmanAutoUpdateResult } from '#lib/types/podman.js';
	import { handleApiResultWithCallbacks } from '#lib/utils/api.js';
	import { destinationEnvironmentId } from '#lib/utils/environments.js';
	import { withOccurrenceKeys } from '#lib/utils/formatting.js';
	import { shownPodmanEnvironments } from '#lib/utils/podman.js';
	import { tryCatch } from '#lib/utils/try-catch.js';

	let { data } = $props();

	const podmanEnvs = $derived(shownPodmanEnvironments(data.podmanStatuses));
	let picked = $state('');
	const environmentId = $derived(destinationEnvironmentId(picked, PERMISSIONS.PODMAN_AUTOUPDATE, podmanEnvs));
	const status = $derived(data.podmanStatuses[environmentId]);
	const mode = $derived(status?.autoUpdateMode ?? 'unavailable');

	let dryRun = $state(false);
	let rollback = $state(true);
	let running = $state(false);
	let result = $state<PodmanAutoUpdateResult | null>(null);

	$effect(() => {
		void environmentId;
		result = null;
	});

	async function handleRun() {
		const targetId = environmentId;
		running = true;
		await handleApiResultWithCallbacks({
			result: await tryCatch(podmanService.runAutoUpdate(mode === 'rest' ? { dryRun, rollback } : {}, targetId)),
			message: m.podman_autoupdate_failed(),
			setLoadingState: (value) => (running = value),
			onSuccess: (response) => {
				if ((response.errors ?? []).length > 0) {
					toast.error(m.podman_autoupdate_partial(), { description: response.errors?.join('\n') });
				} else {
					toast.success(m.podman_autoupdate_success());
				}
				if (targetId !== environmentId) return;
				result = response;
			}
		});
	}
</script>

<ResourcePageLayout title={m.auto_update()} subtitle={m.podman_autoupdate_subtitle()} icon={UpdateIcon}>
	{#snippet mainContent()}
		{#if podmanEnvs.length > 0}
			<div class="mb-4 flex justify-end">
				<EnvironmentDestinationSelect
					bind:value={() => environmentId, (value) => (picked = value)}
					permission={PERMISSIONS.PODMAN_AUTOUPDATE}
					environments={podmanEnvs}
				/>
			</div>
		{/if}
		{#if mode === 'unavailable'}
			<EmptyState
				icon={UpdateIcon}
				title={m.common_unavailable()}
				description={status?.systemdReason || m.podman_autoupdate_unavailable()}
			/>
		{:else}
			<div class="space-y-4">
				<p class="text-sm text-muted-foreground">
					{mode === 'rest' ? m.podman_autoupdate_rest_description() : m.podman_autoupdate_systemd_description()}
				</p>
				<div class="flex flex-wrap items-center gap-x-6 gap-y-3">
					{#if mode === 'rest'}
						<LabeledSwitch
							id="autoupdate-dry-run"
							label={m.podman_autoupdate_dry_run()}
							description={m.podman_autoupdate_dry_run_description()}
							bind:checked={dryRun}
						/>
						<LabeledSwitch id="autoupdate-rollback" label={m.podman_autoupdate_rollback()} bind:checked={rollback} />
					{/if}
					<ArcaneButton
						action="update"
						customLabel={m.podman_autoupdate_run()}
						onclick={handleRun}
						loading={running}
						disabled={running}
					/>
				</div>

				{#if result}
					{#if result.unit}
						<DetailSection title={m.podman_unit()} icon={InfoIcon}>
							<KeyValueGrid>
								<KeyValueCard label={m.podman_unit()}>{result.unit.name}</KeyValueCard>
								<KeyValueCard label={m.podman_unit_active_state()}>
									{result.unit.activeState} ({result.unit.subState})
								</KeyValueCard>
							</KeyValueGrid>
						</DetailSection>
					{/if}

					{#if (result.reports ?? []).length > 0}
						<div class="overflow-x-auto rounded-lg border">
							<Table.Root aria-label={m.auto_update()}>
								<Table.Header>
									<Table.Row>
										<Table.Head scope="col">{m.containers()}</Table.Head>
										<Table.Head scope="col">{m.common_image()}</Table.Head>
										<Table.Head scope="col">{m.podman_autoupdate_policy()}</Table.Head>
										<Table.Head scope="col">{m.podman_unit()}</Table.Head>
										<Table.Head scope="col">{m.common_status()}</Table.Head>
									</Table.Row>
								</Table.Header>
								<Table.Body>
									{#each result.reports ?? [] as report (report.containerId)}
										<Table.Row>
											<Table.Cell>{report.containerName}</Table.Cell>
											<Table.Cell><span class="font-mono text-xs">{report.imageName}</span></Table.Cell>
											<Table.Cell>{report.policy}</Table.Cell>
											<Table.Cell><span class="font-mono text-xs">{report.systemdUnit}</span></Table.Cell>
											<Table.Cell>
												<Badge variant={report.updated === 'true' ? 'green' : 'gray'} size="sm">{report.updated}</Badge>
											</Table.Cell>
										</Table.Row>
									{/each}
								</Table.Body>
							</Table.Root>
						</div>
					{:else if result.mode === 'rest'}
						<EmptyState icon={UpdateIcon} title={m.podman_autoupdate_no_reports()} variant="plain" />
					{/if}

					{#if (result.errors ?? []).length > 0}
						<DetailSection title={m.common_error()} icon={AlertIcon}>
							<ul class="space-y-1 font-mono text-xs text-destructive">
								{#each withOccurrenceKeys(result.errors) as error (error.key)}
									<li>{error.value}</li>
								{/each}
							</ul>
						</DetailSection>
					{/if}
				{/if}
			</div>
		{/if}
	{/snippet}
</ResourcePageLayout>
