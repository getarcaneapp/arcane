<script lang="ts">
	import { createQuery } from '@tanstack/svelte-query';

	import { ProcessesCard } from '#lib/components/resource-detail/index.js';
	import { m } from '#lib/paraglide/messages.js';
	import { podmanPodTopQuery } from '#lib/query/podman.js';

	interface Props {
		podId: string;
		envId: string;
		isRunning: boolean;
	}

	let { podId, envId, isRunning }: Props = $props();

	const topQuery = createQuery(() => ({
		...podmanPodTopQuery(envId, podId),
		enabled: isRunning,
		retry: false
	}));
</script>

<ProcessesCard
	description={m.podman_pod_processes_description()}
	unavailableDescription={m.podman_pod_not_running()}
	canFetch={isRunning}
	isPending={topQuery.isPending}
	isFetching={topQuery.isFetching}
	isError={topQuery.isError}
	error={topQuery.error}
	titles={topQuery.data?.titles}
	processes={topQuery.data?.processes}
	onRefresh={() => topQuery.refetch()}
/>
