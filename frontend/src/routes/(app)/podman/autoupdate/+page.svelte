<script lang="ts">
	import { toast } from 'svelte-sonner';

	import { ArcaneButton } from '#lib/components/arcane-button/index.js';
	import LabeledSwitch from '#lib/components/form/labeled-switch.svelte';
	import { DetailSection, KeyValueCard, KeyValueGrid } from '#lib/components/resource-detail/index.js';
	import EmptyState from '#lib/components/states/empty-state.svelte';
	import { Badge } from '#lib/components/ui/badge/index.js';
	import * as Card from '#lib/components/ui/card/index.js';
	import * as Table from '#lib/components/ui/table/index.js';
	import { useEnvironmentRefresh } from '#lib/hooks/use-environment-refresh.svelte.js';
	import { AlertIcon, InfoIcon, UpdateIcon } from '#lib/icons/index.js';
	import { ResourcePageLayout } from '#lib/layouts/index.js';
	import { m } from '#lib/paraglide/messages.js';
	import { podmanService } from '#lib/services/podman-service.js';
	import type { PodmanAutoUpdateResult } from '#lib/types/podman.js';
	import { handleApiResultWithCallbacks } from '#lib/utils/api.js';
	import { withOccurrenceKeys } from '#lib/utils/formatting.js';
	import { tryCatch } from '#lib/utils/try-catch.js';

	let { data } = $props();

	const status = $derived(data.podmanStatus);
	const mode = $derived(status?.autoUpdateMode ?? 'unavailable');

	let dryRun = $state(false);
	let rollback = $state(true);
	let running = $state(false);
	let result = $state<PodmanAutoUpdateResult | null>(null);
	let session = 0;

	useEnvironmentRefresh(() => {
		session += 1;
		result = null;
	});

	async function handleRun() {
		const started = session;
		running = true;
		await handleApiResultWithCallbacks({
			result: await tryCatch(podmanService.runAutoUpdate(mode === 'rest' ? { dryRun, rollback } : {})),
			message: m.podman_autoupdate_failed(),
			setLoadingState: (value) => (running = value),
			onSuccess: (response) => {
				toast.success(m.podman_autoupdate_success());
				if (started !== session) return;
				result = response;
			}
		});
	}
</script>

<ResourcePageLayout title={m.auto_update()} subtitle={m.podman_autoupdate_subtitle()} icon={UpdateIcon} environmentPicker>
	{#snippet mainContent()}
		{#if mode === 'unavailable'}
			<EmptyState
				icon={UpdateIcon}
				title={m.common_unavailable()}
				description={status?.systemdReason || m.podman_autoupdate_unavailable()}
			/>
		{:else}
			<div class="space-y-4">
				<Card.Root>
					<Card.Header>
						<Card.Title>{m.podman_autoupdate_run()}</Card.Title>
						<Card.Description>
							{mode === 'rest' ? m.podman_autoupdate_rest_description() : m.podman_autoupdate_systemd_description()}
						</Card.Description>
					</Card.Header>
					<Card.Content>
						<div class="space-y-4">
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
					</Card.Content>
				</Card.Root>

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
