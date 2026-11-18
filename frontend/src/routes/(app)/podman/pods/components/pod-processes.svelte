<script lang="ts">
	import * as Card from '@arcane/ui/card';
	import { Spinner } from '@arcane/ui/spinner';
	import * as Table from '@arcane/ui/table';
	import { createQuery } from '@tanstack/svelte-query';

	import { ArcaneButton } from '#lib/components/arcane-button/index.js';
	import EmptyState from '#lib/components/states/empty-state.svelte';
	import MonoCell from '#lib/components/vulnerability/mono-cell.svelte';
	import { LayoutListIcon } from '#lib/icons/index.js';
	import { m } from '#lib/paraglide/messages.js';
	import { queryKeys } from '#lib/query/query-keys.js';
	import { podmanService } from '#lib/services/podman-service.js';
	import { extractApiErrorMessage } from '#lib/utils/api.js';
	import { withOccurrenceKeys } from '#lib/utils/formatting.js';

	interface Props {
		podId: string;
		envId: string;
		isRunning: boolean;
	}

	let { podId, envId, isRunning }: Props = $props();

	const topQuery = createQuery(() => ({
		queryKey: queryKeys.podman.podTop(envId, podId),
		queryFn: () => podmanService.getPodTop(podId, envId),
		enabled: isRunning,
		retry: false
	}));

	const processes = $derived(topQuery.data?.processes ?? []);
	const pidIndex = $derived((topQuery.data?.titles ?? []).findIndex((title) => title.toUpperCase() === 'PID'));
</script>

<Card.Root>
	<Card.Header icon={LayoutListIcon}>
		<div class="flex flex-col space-y-1.5">
			<Card.Title>
				<h2>{m.containers_processes_title()}</h2>
			</Card.Title>
			<Card.Description>{m.podman_pod_processes_description()}</Card.Description>
		</div>
		{#if isRunning}
			<div class="ml-auto">
				<ArcaneButton action="refresh" disabled={topQuery.isFetching} onclick={() => topQuery.refetch()} />
			</div>
		{/if}
	</Card.Header>
	{#if !isRunning}
		<EmptyState icon={LayoutListIcon} title={m.common_unavailable()} description={m.podman_pod_not_running()} />
	{:else if topQuery.isError}
		<EmptyState
			icon={LayoutListIcon}
			title={m.common_load_failed({ resource: m.containers_processes_title() })}
			description={extractApiErrorMessage(topQuery.error)}
			actionLabel={m.common_retry()}
			onAction={() => topQuery.refetch()}
		/>
	{:else if topQuery.isPending}
		<div class="flex items-center justify-center gap-2 py-12 text-sm text-muted-foreground" role="status" aria-live="polite">
			<Spinner tone="muted" />
			<span>{m.common_loading()}</span>
		</div>
	{:else if processes.length === 0}
		<EmptyState icon={LayoutListIcon} title={m.containers_processes_empty()} />
	{:else}
		<div class="overflow-x-auto rounded-b-lg">
			<Table.Root aria-label={m.containers_processes_title()}>
				<Table.Header>
					<Table.Row>
						{#each withOccurrenceKeys(topQuery.data?.titles ?? []) as title (title.key)}
							<Table.Head scope="col">{title.value}</Table.Head>
						{/each}
					</Table.Row>
				</Table.Header>
				<Table.Body>
					{#each processes as cells (pidIndex >= 0 ? cells[pidIndex] : cells)}
						<Table.Row>
							{#each withOccurrenceKeys(cells) as cell (cell.key)}
								<Table.Cell><MonoCell value={cell.value} /></Table.Cell>
							{/each}
						</Table.Row>
					{/each}
				</Table.Body>
			</Table.Root>
		</div>
	{/if}
</Card.Root>
