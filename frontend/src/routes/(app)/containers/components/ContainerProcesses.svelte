<script lang="ts">
	import * as Card from '#lib/components/ui/card/index.js';
	import * as Table from '#lib/components/ui/table/index.js';
	import { ArcaneButton } from '#lib/components/arcane-button/index.js';
	import EmptyState from '#lib/components/states/empty-state.svelte';
	import MonoCell from '#lib/components/vulnerability/mono-cell.svelte';
	import Spinner from '#lib/components/ui/spinner/spinner.svelte';
	import { m } from '#lib/paraglide/messages.js';
	import { LayoutListIcon } from '#lib/icons/index.js';
	import { createQuery } from '@tanstack/svelte-query';
	import { queryKeys } from '#lib/query/query-keys.js';
	import { containerService } from '#lib/services/container-service.js';
	import { environmentStore } from '#lib/stores/environment.store.svelte.js';
	import { extractApiErrorMessage } from '#lib/utils/api.js';

	interface Props {
		containerId: string;
		status: string;
	}

	let { containerId, status }: Props = $props();

	const canFetch = $derived(status === 'running' || status === 'paused');

	const processesQuery = createQuery(() => ({
		queryKey: queryKeys.containers.processes(environmentStore.selected?.id ?? '', containerId),
		queryFn: ({ signal }) => containerService.getContainerProcesses(containerId, signal),
		enabled: canFetch,
		gcTime: 0,
		retry: false,
		refetchOnWindowFocus: false,
		refetchOnReconnect: false,
		refetchInterval: 5000
	}));

	const titles = $derived(processesQuery.data?.titles ?? []);
	const processes = $derived(processesQuery.data?.processes ?? []);
	const pidIndex = $derived(titles.findIndex((title) => title.toUpperCase() === 'PID'));
</script>

<Card.Root>
	<Card.Header icon={LayoutListIcon}>
		<div class="flex flex-col space-y-1.5">
			<Card.Title>
				<h2>{m.containers_processes_title()}</h2>
			</Card.Title>
			<Card.Description>{m.containers_processes_description()}</Card.Description>
		</div>
		{#if canFetch}
			<div class="ml-auto">
				<ArcaneButton action="refresh" size="sm" disabled={processesQuery.isFetching} onclick={() => processesQuery.refetch()} />
			</div>
		{/if}
	</Card.Header>
	{#if !canFetch}
		<EmptyState icon={LayoutListIcon} title={m.common_unavailable()} description={m.containers_processes_unavailable()} />
	{:else if processesQuery.isError}
		<EmptyState
			icon={LayoutListIcon}
			title={m.common_load_failed({ resource: m.containers_processes_title() })}
			description={extractApiErrorMessage(processesQuery.error)}
			actionLabel={m.common_retry()}
			onAction={() => processesQuery.refetch()}
		/>
	{:else if processesQuery.isPending}
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
						{#each titles as title}
							<Table.Head scope="col">{title}</Table.Head>
						{/each}
					</Table.Row>
				</Table.Header>
				<Table.Body>
					{#each processes as cells (pidIndex >= 0 ? cells[pidIndex] : cells)}
						<Table.Row>
							{#each cells as cell}
								<Table.Cell><MonoCell value={cell} /></Table.Cell>
							{/each}
						</Table.Row>
					{/each}
				</Table.Body>
			</Table.Root>
		</div>
	{/if}
</Card.Root>
