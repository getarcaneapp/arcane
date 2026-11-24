<script lang="ts">
	import { ArcaneButton } from '#lib/components/arcane-button/index.js';
	import EmptyState from '#lib/components/states/empty-state.svelte';
	import * as Card from '#lib/components/ui/card/index.js';
	import Spinner from '#lib/components/ui/spinner/spinner.svelte';
	import * as Table from '#lib/components/ui/table/index.js';
	import MonoCell from '#lib/components/vulnerability/mono-cell.svelte';
	import { LayoutListIcon } from '#lib/icons/index.js';
	import { m } from '#lib/paraglide/messages.js';
	import { extractApiErrorMessage } from '#lib/utils/api.js';
	import { withOccurrenceKeys } from '#lib/utils/formatting.js';

	interface Props {
		description: string;
		unavailableDescription: string;
		canFetch: boolean;
		isPending: boolean;
		isFetching: boolean;
		isError: boolean;
		error: unknown;
		titles?: string[] | null;
		processes?: string[][] | null;
		onRefresh: () => void;
	}

	let {
		description,
		unavailableDescription,
		canFetch,
		isPending,
		isFetching,
		isError,
		error,
		titles: rawTitles,
		processes: rawProcesses,
		onRefresh
	}: Props = $props();

	const titles = $derived(rawTitles ?? []);
	const processes = $derived(rawProcesses ?? []);
	const pidIndex = $derived(titles.findIndex((title) => title.toUpperCase() === 'PID'));
	const processKeys = $derived(
		withOccurrenceKeys(
			processes.map((cells) => {
				if (pidIndex >= 0) {
					return cells[pidIndex] ?? '';
				}
				return JSON.stringify(cells);
			})
		)
	);
</script>

<Card.Root>
	<Card.Header icon={LayoutListIcon}>
		<div class="flex flex-col space-y-1.5">
			<Card.Title>
				<h2>{m.containers_processes_title()}</h2>
			</Card.Title>
			<Card.Description>{description}</Card.Description>
		</div>
		{#if canFetch}
			<div class="ml-auto">
				<ArcaneButton action="refresh" disabled={isFetching} onclick={onRefresh} />
			</div>
		{/if}
	</Card.Header>
	{#if !canFetch}
		<EmptyState icon={LayoutListIcon} title={m.common_unavailable()} description={unavailableDescription} />
	{:else if isError}
		<EmptyState
			icon={LayoutListIcon}
			title={m.common_load_failed({ resource: m.containers_processes_title() })}
			description={extractApiErrorMessage(error)}
			actionLabel={m.common_retry()}
			onAction={onRefresh}
		/>
	{:else if isPending}
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
						{#each withOccurrenceKeys(titles) as title (title.key)}
							<Table.Head scope="col">{title.value}</Table.Head>
						{/each}
					</Table.Row>
				</Table.Header>
				<Table.Body>
					{#each processes as cells, index (processKeys[index]?.key ?? index)}
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
