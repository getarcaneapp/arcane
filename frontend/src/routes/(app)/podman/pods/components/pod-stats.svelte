<script lang="ts">
	import { onDestroy } from 'svelte';

	import EmptyState from '#lib/components/states/empty-state.svelte';
	import * as Card from '#lib/components/ui/card/index.js';
	import * as Table from '#lib/components/ui/table/index.js';
	import MonoCell from '#lib/components/vulnerability/mono-cell.svelte';
	import { StatsIcon } from '#lib/icons/index.js';
	import { m } from '#lib/paraglide/messages.js';
	import type { PodmanPodInspectContainer } from '#lib/types/podman.js';
	import { rowKey } from '#lib/utils/environments.js';
	import { bytes } from '#lib/utils/formatting.js';

	import ContainerStatsCell from '../../../containers/components/container-stats-cell.svelte';
	import { ContainerStatsManager } from '../../../containers/components/container-stats-manager.svelte';
	import ContainerStatsSync from '../../../containers/components/container-stats-sync.svelte';

	interface Props {
		containers: PodmanPodInspectContainer[];
		envId: string;
		isRunning: boolean;
	}

	let { containers, envId, isRunning }: Props = $props();

	const statsManager = new ContainerStatsManager();
	onDestroy(() => statsManager.destroy());

	const running = $derived(
		isRunning
			? containers
					.filter((container) => container.state.toLowerCase() === 'running')
					.map((container) => ({
						...container,
						key: rowKey({ id: container.id, environmentId: envId, environmentTagged: true })
					}))
			: []
	);
	const targetIds = $derived(new Set(running.map((container) => container.key)));
</script>

<ContainerStatsSync {statsManager} {targetIds} />

<Card.Root>
	<Card.Header icon={StatsIcon}>
		<div class="flex flex-col space-y-1.5">
			<Card.Title>
				<h2>{m.containers_resource_metrics()}</h2>
			</Card.Title>
			<Card.Description>{m.podman_pod_stats_description()}</Card.Description>
		</div>
	</Card.Header>
	{#if running.length === 0}
		<EmptyState icon={StatsIcon} title={m.common_unavailable()} description={m.podman_pod_not_running()} />
	{:else}
		<div class="overflow-x-auto rounded-b-lg">
			<Table.Root aria-label={m.containers_resource_metrics()}>
				<Table.Header>
					<Table.Row>
						<Table.Head scope="col">{m.common_name()}</Table.Head>
						<Table.Head scope="col">{m.cpu_usage()}</Table.Head>
						<Table.Head scope="col">{m.memory_usage()}</Table.Head>
						<Table.Head scope="col">{m.containers_block_io()}</Table.Head>
						<Table.Head scope="col">{m.containers_network_io()}</Table.Head>
						<Table.Head scope="col">{m.podman_pids()}</Table.Head>
					</Table.Row>
				</Table.Header>
				<Table.Body>
					{#each running as container (container.key)}
						{@const memory = statsManager.getMemoryUsage(container.key)}
						{@const sample = statsManager.getSample(container.key)}
						{@const loading = statsManager.isLoading(container.key)}
						{@const unavailable = statsManager.hasError(container.key)}
						<Table.Row>
							<Table.Cell>{container.name}</Table.Cell>
							<Table.Cell>
								<ContainerStatsCell type="cpu" value={statsManager.getCPUPercent(container.key)} {loading} {unavailable} />
							</Table.Cell>
							<Table.Cell>
								<ContainerStatsCell type="memory" value={memory?.usage} limit={memory?.limit} {loading} {unavailable} />
							</Table.Cell>
							<Table.Cell>
								<MonoCell
									value={sample ? `${bytes.format(sample.blockReadBytes)} / ${bytes.format(sample.blockWriteBytes)}` : ''}
								/>
							</Table.Cell>
							<Table.Cell>
								<MonoCell
									value={sample ? `${bytes.format(sample.networkRxBytes)} / ${bytes.format(sample.networkTxBytes)}` : ''}
								/>
							</Table.Cell>
							<Table.Cell><MonoCell value={sample ? String(sample.pids) : ''} /></Table.Cell>
						</Table.Row>
					{/each}
				</Table.Body>
			</Table.Root>
		</div>
	{/if}
</Card.Root>
