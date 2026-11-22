<script lang="ts">
	import { createQuery } from '@tanstack/svelte-query';

	import { ProcessesCard } from '#lib/components/resource-detail/index.js';
	import { m } from '#lib/paraglide/messages.js';
	import { containerProcessesQuery } from '#lib/query/containers.js';

	interface Props {
		containerId: string;
		environmentId: string;
		status: string;
	}

	let { containerId, environmentId, status }: Props = $props();

	const canFetch = $derived(status === 'running' || status === 'paused');

	const processesQuery = createQuery(() => ({
		...containerProcessesQuery(environmentId, containerId),
		enabled: canFetch,
		gcTime: 0,
		retry: false,
		refetchOnWindowFocus: false,
		refetchOnReconnect: false
	}));
</script>

<ProcessesCard
	description={m.containers_processes_description()}
	unavailableDescription={m.containers_processes_unavailable()}
	{canFetch}
	isPending={processesQuery.isPending}
	isFetching={processesQuery.isFetching}
	isError={processesQuery.isError}
	error={processesQuery.error}
	titles={processesQuery.data?.titles}
	processes={processesQuery.data?.processes}
	onRefresh={() => processesQuery.refetch()}
/>
