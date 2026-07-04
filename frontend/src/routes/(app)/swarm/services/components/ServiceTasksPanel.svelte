<script lang="ts">
	import StatusBadge from '$lib/components/badges/status-badge.svelte';
	import { ArcaneButton } from '$lib/components/arcane-button/index.js';
	import { m } from '$lib/paraglide/messages';
	import { swarmService } from '$lib/services/swarm-service';
	import type { SwarmTaskSummary } from '$lib/types/swarm';
	import { getSwarmTaskStateVariant, sortSwarmTasks } from '$lib/utils/swarm-tasks';
	import { JobsIcon, ConnectionIcon } from '$lib/icons';
	import DetailPanel from '$lib/components/resource-detail/detail-panel.svelte';

	let {
		serviceName,
		serviceId
	}: {
		serviceName: string;
		serviceId: string;
	} = $props();

	let tasks = $state<SwarmTaskSummary[]>([]);
	let isLoading = $state(false);
	let hasLoaded = $state(false);

	async function loadTasks() {
		isLoading = true;
		try {
			const result = await swarmService.getServiceTasks(serviceId, {
				pagination: { page: 1, limit: 100 }
			});
			tasks = sortSwarmTasks(result.data ?? []);
		} catch (err) {
			console.error(m.swarm_service_tasks_load_failed_log(), err);
		} finally {
			isLoading = false;
			hasLoaded = true;
		}
	}

	$effect(() => {
		if (serviceName && serviceId && !hasLoaded) {
			loadTasks();
		}
	});
</script>

<DetailPanel>
	<section class="scroll-mt-24 p-4 sm:p-5">
		<div class="mb-3 flex items-start justify-between gap-3">
			<div class="flex items-start gap-2">
				<JobsIcon class="text-primary mt-0.5 size-4 shrink-0" />
				<div class="min-w-0">
					<h3 class="text-sm font-semibold">{m.swarm_tasks_title()}</h3>
					<p class="text-muted-foreground text-xs">
						{m.swarm_service_tasks_count({ count: tasks.length })}
					</p>
				</div>
			</div>
			<ArcaneButton action="refresh" size="sm" onclick={loadTasks} disabled={isLoading}>
				{m.common_refresh()}
			</ArcaneButton>
		</div>

		{#if isLoading && !hasLoaded}
			<div class="text-muted-foreground py-12 text-center text-sm">{m.swarm_service_tasks_loading()}</div>
		{:else if tasks.length === 0}
			<div class="text-muted-foreground rounded-lg border border-dashed py-12 text-center">
				<div class="bg-muted/30 mx-auto mb-4 flex size-16 items-center justify-center rounded-full">
					<JobsIcon class="text-muted-foreground size-6" />
				</div>
				<div class="text-sm">{m.swarm_service_tasks_empty()}</div>
			</div>
		{:else}
			<div class="divide-border/50 divide-y">
				{#each tasks as task (task.id)}
					<div class="space-y-3 py-4 first:pt-0 last:pb-0">
						<div class="flex items-center justify-between gap-3">
							<div class="min-w-0 flex-1">
								<div class="text-foreground truncate text-sm font-semibold" title={task.name}>
									{task.name}
								</div>
								<div class="text-muted-foreground font-mono text-xs">{task.id.slice(0, 12)}</div>
							</div>
							<StatusBadge text={task.currentState} variant={getSwarmTaskStateVariant(task.currentState)} />
						</div>
						<div class="grid grid-cols-1 gap-x-6 gap-y-4 sm:grid-cols-2">
							<div>
								<div class="text-muted-foreground text-[11px] font-semibold tracking-wide uppercase">
									{m.swarm_node()}
								</div>
								<div class="mt-1 flex items-center gap-1">
									<ConnectionIcon class="text-muted-foreground size-3" />
									<span class="text-foreground truncate text-sm font-medium">{task.nodeName || m.common_na()}</span>
								</div>
							</div>
							<div>
								<div class="text-muted-foreground text-[11px] font-semibold tracking-wide uppercase">
									{m.swarm_desired_state()}
								</div>
								<div class="mt-1">
									<StatusBadge text={task.desiredState} variant={getSwarmTaskStateVariant(task.desiredState)} size="sm" />
								</div>
							</div>
							{#if task.error}
								<div class="sm:col-span-2">
									<div class="text-muted-foreground text-[11px] font-semibold tracking-wide uppercase">{m.common_error()}</div>
									<div class="mt-1 text-sm break-all text-red-400">{task.error}</div>
								</div>
							{/if}
						</div>
					</div>
				{/each}
			</div>
		{/if}
	</section>
</DetailPanel>
