<script lang="ts">
	import { createQuery } from '@tanstack/svelte-query';

	import { ArcaneButton } from '#lib/components/arcane-button/index.js';
	import * as Alert from '#lib/components/ui/alert/index.js';
	import { Badge } from '#lib/components/ui/badge/index.js';
	import * as Card from '#lib/components/ui/card/index.js';
	import { ResponsiveDialog } from '#lib/components/ui/responsive-dialog/index.js';
	import { AlertTriangleIcon, SettingsIcon } from '#lib/icons/index.js';
	import { ResourcePageLayout, type ActionButton } from '#lib/layouts/index.js';
	import { m } from '#lib/paraglide/messages.js';
	import { queryKeys } from '#lib/query/query-keys.js';
	import { swarmService } from '#lib/services/swarm-service.js';
	import type { EnvironmentRef } from '#lib/stores/environment.store.svelte.js';
	import { hasPermission } from '#lib/utils/auth.js';
	import { readableEnvironments, sumAcrossEnvironments } from '#lib/utils/environments.js';

	import ClusterPanel from './components/cluster-panel.svelte';
	import EasyJoinDialog from './components/easy-join-dialog.svelte';

	const statusQuery = createQuery(() => {
		const environments = readableEnvironments('swarm:read');
		return {
			queryKey: queryKeys.swarm.status(environments.map((environment) => environment.id).join(',')),
			queryFn: () =>
				sumAcrossEnvironments(environments, (scope, config) => swarmService.getSwarmStatus(scope, config), {
					enabled: false
				}),
			select: (status) => ({
				failedEnvironments: status.failedEnvironments,
				rows: environments.map((environment) => ({
					environment,
					status: status.byEnvironment.find((entry) => entry.environment.id === environment.id)?.data
				}))
			})
		};
	});
	const rows = $derived(statusQuery.data?.rows ?? []);
	const failedEnvironments = $derived(statusQuery.data?.failedEnvironments ?? []);

	let panelEnvironment = $state<EnvironmentRef | null>(null);
	let panelOpen = $state(false);
	let easyJoinManagerId = $state<string | undefined>(undefined);
	let easyJoinDialogOpen = $state(false);
	let easyJoinSession = $state(0);

	function openPanel(environment: EnvironmentRef) {
		panelEnvironment = environment;
		panelOpen = true;
	}

	function openEasyJoin(environmentId: string) {
		easyJoinManagerId = environmentId;
		easyJoinSession += 1;
		easyJoinDialogOpen = true;
	}

	async function refresh() {
		await statusQuery.refetch();
	}

	const actionButtons: ActionButton[] = $derived([
		{
			id: 'refresh',
			action: 'refresh',
			placement: 'secondary',
			iconOnly: true,
			label: m.common_refresh(),
			onclick: refresh,
			loading: statusQuery.isFetching,
			disabled: statusQuery.isFetching
		}
	]);
</script>

<ResourcePageLayout title={m.cluster()} subtitle={m.swarm_cluster_subtitle()} icon={SettingsIcon} class="pb-6" {actionButtons}>
	{#snippet mainContent()}
		<div class="space-y-4 pb-6">
			{#if failedEnvironments.length > 0}
				<Alert.Root variant="warning-subtle" size="sm" icon={AlertTriangleIcon}>
					<div class="flex flex-wrap items-center justify-between gap-2">
						<Alert.Description>
							{m.all_environments_unavailable({
								environments: failedEnvironments.map(({ environment, message }) => `${environment.name} (${message})`).join(', ')
							})}
						</Alert.Description>
						<ArcaneButton action="refresh" size="sm" customLabel={m.common_retry()} onclick={refresh} />
					</div>
				</Alert.Root>
			{/if}

			<Card.Root>
				<Card.Content>
					<div class="divide-y divide-border/60 text-sm">
						{#each rows as row (row.environment.id)}
							<div class="flex flex-wrap items-center justify-between gap-3 py-3">
								<div class="flex min-w-0 flex-col gap-1">
									<span class="font-medium">{row.environment.name}</span>
									{#if row.status?.clusterId}
										<span class="font-mono text-xs break-all text-muted-foreground">
											{m.swarm_cluster_id_label()}: {row.status.clusterId}
										</span>
									{/if}
								</div>
								<div class="flex flex-wrap items-center gap-2">
									{#if row.status?.clusterId}
										<Badge variant="purple">{m.manager()}</Badge>
									{:else if row.status?.enabled}
										<Badge variant="blue">{m.worker()}</Badge>
									{:else}
										<Badge variant="gray">{m.swarm_cluster_not_initialized()}</Badge>
									{/if}
									{#if row.status?.clusterId && hasPermission('swarm:nodes', row.environment.id) && hasPermission('swarm:unlock', row.environment.id)}
										<ArcaneButton
											action="create"
											size="sm"
											customLabel={m.swarm_easy_join_action()}
											onclick={() => openEasyJoin(row.environment.id)}
										/>
									{/if}
									<ArcaneButton
										action="inspect"
										size="sm"
										customLabel={m.common_inspect()}
										onclick={() => openPanel(row.environment)}
									/>
								</div>
							</div>
						{/each}
					</div>
				</Card.Content>
			</Card.Root>
		</div>

		{#if panelEnvironment}
			{@const environment = panelEnvironment}
			<ResponsiveDialog
				bind:open={panelOpen}
				variant="sheet"
				title={environment.name}
				description={m.swarm_cluster_subtitle()}
				contentClass="sm:max-w-5xl"
			>
				{#snippet children()}
					{#key environment.id}
						<div class="px-6 py-6">
							<ClusterPanel environmentId={environment.id} onChanged={refresh} />
						</div>
					{/key}
				{/snippet}
			</ResponsiveDialog>
		{/if}

		{#key easyJoinSession}
			<EasyJoinDialog bind:open={easyJoinDialogOpen} managerEnvironmentId={easyJoinManagerId} onComplete={refresh} />
		{/key}
	{/snippet}
</ResourcePageLayout>
