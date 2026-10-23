<script lang="ts">
	import { toast } from 'svelte-sonner';

	import { ArcaneButton } from '#lib/components/arcane-button/index.js';
	import LabeledSwitch from '#lib/components/form/labeled-switch.svelte';
	import { DetailSection, KeyValueCard, KeyValueGrid } from '#lib/components/resource-detail/index.js';
	import EmptyState from '#lib/components/states/empty-state.svelte';
	import { Badge } from '#lib/components/ui/badge/index.js';
	import * as Table from '#lib/components/ui/table/index.js';
	import { AlertIcon, InfoIcon, UpdateIcon } from '#lib/icons/index.js';
	import { ResourcePageLayout } from '#lib/layouts/index.js';
	import { m } from '#lib/paraglide/messages.js';
	import { podmanService } from '#lib/services/podman-service.js';
	import type { EnvironmentRef } from '#lib/stores/environment.store.svelte.js';
	import type { PodmanAutoUpdateMode, PodmanAutoUpdateResult } from '#lib/types/podman.js';
	import { handleApiResultWithCallbacks } from '#lib/utils/api.js';
	import { actionEnvironments, settleAcrossEnvironments } from '#lib/utils/environments.js';
	import { withOccurrenceKeys } from '#lib/utils/formatting.js';
	import { shownPodmanEnvironments } from '#lib/utils/podman.js';
	import { tryCatch } from '#lib/utils/try-catch.js';

	let { data } = $props();

	const podmanEnvs = $derived(shownPodmanEnvironments(data.podmanStatuses));
	const runnableEnvs = $derived(
		actionEnvironments('podman:autoupdate').filter(
			(env) => podmanEnvs.some((podmanEnv) => podmanEnv.id === env.id) && modeOf(env.id) !== 'unavailable'
		)
	);
	const hasRest = $derived(runnableEnvs.some((env) => modeOf(env.id) === 'rest'));

	let dryRun = $state(false);
	let rollback = $state(true);
	let running = $state(false);
	let results = $state<{ environment: EnvironmentRef; value: PodmanAutoUpdateResult }[]>([]);

	function modeOf(environmentId: string): PodmanAutoUpdateMode {
		return data.podmanStatuses[environmentId]?.autoUpdateMode ?? 'unavailable';
	}

	function modeLabel(mode: PodmanAutoUpdateMode): string {
		switch (mode) {
			case 'rest':
				return m.podman_autoupdate_mode_rest();
			case 'systemd':
				return m.podman_autoupdate_mode_systemd();
			default:
				return m.common_unavailable();
		}
	}

	async function handleRun() {
		running = true;
		await handleApiResultWithCallbacks({
			result: await tryCatch(
				settleAcrossEnvironments(
					runnableEnvs.filter((env) => !dryRun || modeOf(env.id) === 'rest'),
					(id) => podmanService.runAutoUpdate(modeOf(id) === 'rest' ? { dryRun, rollback } : {}, id)
				)
			),
			message: m.podman_autoupdate_failed(),
			setLoadingState: (value) => (running = value),
			onSuccess: (settled) => {
				results = settled;
				const errors = settled.flatMap(({ environment, value }) =>
					(value.errors ?? []).map((error) => `${environment.name}: ${error}`)
				);
				if (errors.length > 0) {
					toast.error(m.podman_autoupdate_partial(), { description: errors.join('\n') });
				} else {
					toast.success(m.podman_autoupdate_success());
				}
			}
		});
	}
</script>

<ResourcePageLayout title={m.auto_update()} subtitle={m.podman_autoupdate_subtitle()} icon={UpdateIcon}>
	{#snippet mainContent()}
		{#if podmanEnvs.length === 0}
			<EmptyState icon={UpdateIcon} title={m.common_unavailable()} description={m.podman_autoupdate_unavailable()} />
		{:else}
			<div class="space-y-4">
				<div class="overflow-x-auto rounded-lg border">
					<Table.Root aria-label={m.auto_update()}>
						<Table.Header>
							<Table.Row>
								<Table.Head scope="col">{m.resource_environment_cap()}</Table.Head>
								<Table.Head scope="col">{m.common_version()}</Table.Head>
								<Table.Head scope="col">{m.common_mode()}</Table.Head>
								<Table.Head scope="col">{m.common_status()}</Table.Head>
							</Table.Row>
						</Table.Header>
						<Table.Body>
							{#each podmanEnvs as env (env.id)}
								{@const status = data.podmanStatuses[env.id]}
								{@const mode = modeOf(env.id)}
								<Table.Row>
									<Table.Cell>{env.name}</Table.Cell>
									<Table.Cell><span class="font-mono text-xs">{status?.version ?? ''}</span></Table.Cell>
									<Table.Cell>
										<Badge variant={mode === 'unavailable' ? 'gray' : 'green'} size="sm">{modeLabel(mode)}</Badge>
									</Table.Cell>
									<Table.Cell>
										<span class="text-xs text-muted-foreground">
											{#if mode === 'unavailable'}
												{status?.systemdReason || m.podman_autoupdate_unavailable()}
											{:else if mode === 'rest'}
												{m.podman_autoupdate_rest_description()}
											{:else}
												{m.podman_autoupdate_systemd_description()}
											{/if}
										</span>
									</Table.Cell>
								</Table.Row>
							{/each}
						</Table.Body>
					</Table.Root>
				</div>

				{#if runnableEnvs.length > 0}
					<div class="flex flex-wrap items-center gap-x-6 gap-y-3">
						{#if hasRest}
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
				{/if}

				{#each results as { environment, value: result } (environment.id)}
					<DetailSection title={environment.name} icon={UpdateIcon}>
						<div class="space-y-4">
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
						</div>
					</DetailSection>
				{/each}
			</div>
		{/if}
	{/snippet}
</ResourcePageLayout>
