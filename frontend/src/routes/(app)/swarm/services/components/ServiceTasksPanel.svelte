<script lang="ts">
	import * as Card from '$lib/components/ui/card';
	import StatusBadge from '$lib/components/badges/status-badge.svelte';
	import { ArcaneButton } from '$lib/components/arcane-button/index.js';
	import { m } from '$lib/paraglide/messages';
	import { swarmService } from '$lib/services/swarm-service';
	import type { SwarmTaskSummary } from '$lib/types/swarm.type';
	import { JobsIcon, ConnectionIcon } from '$lib/icons';

	let {
		serviceName
	}: {
		serviceName: string;
	} = $props();

	let tasks = $state<SwarmTaskSummary[]>([]);
	let isLoading = $state(false);
	let hasLoaded = $state(false);

	const STATE_ORDER: Record<string, number> = {
		running: 0,
		starting: 1,
		pending: 2,
		ready: 3,
		complete: 4,
		shutdown: 5,
		failed: 6,
		rejected: 7,
		orphaned: 8,
		remove: 9
	};

	function stateVariant(state: string): 'green' | 'amber' | 'red' | 'gray' {
		if (state === 'running') return 'green';
		if (state === 'pending' || state === 'starting') return 'amber';
		if (state === 'failed' || state === 'rejected' || state === 'shutdown') return 'red';
		return 'gray';
	}

	function sortTasks(raw: SwarmTaskSummary[]): SwarmTaskSummary[] {
		return [...raw].sort((a, b) => {
			const stateA = STATE_ORDER[a.currentState] ?? 99;
			const stateB = STATE_ORDER[b.currentState] ?? 99;
			if (stateA !== stateB) return stateA - stateB;
			return new Date(b.updatedAt).getTime() - new Date(a.updatedAt).getTime();
		});
	}

	async function loadTasks() {
		isLoading = true;
		try {
			const result = await swarmService.getTasks({
				search: serviceName,
				pagination: { page: 1, limit: 100 }
			});
			tasks = sortTasks(result.data ?? []);
		} catch (err) {
			console.error('Failed to load tasks:', err);
		} finally {
			isLoading = false;
			hasLoaded = true;
		}
	}

	$effect(() => {
		if (serviceName && !hasLoaded) {
			loadTasks();
		}
	});
</script>

<Card.Root>
	<Card.Header icon={JobsIcon}>
		<div class="flex flex-1 items-center justify-between">
			<div class="flex flex-col gap-1.5">
				<Card.Title>
					<h2>{m.swarm_tasks_title()}</h2>
				</Card.Title>
				<Card.Description>
					{tasks.length} task{tasks.length !== 1 ? 's' : ''} for this service
				</Card.Description>
			</div>
			<ArcaneButton action="refresh" size="sm" onclick={loadTasks} disabled={isLoading}>
				{m.common_refresh()}
			</ArcaneButton>
		</div>
	</Card.Header>
	<Card.Content class="p-4">
		{#if isLoading && !hasLoaded}
			<div class="text-muted-foreground py-12 text-center text-sm">Loading tasks...</div>
		{:else if tasks.length === 0}
			<div class="text-muted-foreground rounded-lg border border-dashed py-12 text-center">
				<div class="bg-muted/30 mx-auto mb-4 flex size-16 items-center justify-center rounded-full">
					<JobsIcon class="text-muted-foreground size-6" />
				</div>
				<div class="text-sm">No tasks found for this service</div>
			</div>
		{:else}
			<div class="grid grid-cols-1 gap-3 lg:grid-cols-2 xl:grid-cols-3">
				{#each tasks as task (task.id)}
					<Card.Root variant="subtle">
						<Card.Content class="p-4">
							<div class="border-border mb-3 flex items-center justify-between border-b pb-3">
								<div class="min-w-0 flex-1">
									<div class="text-foreground truncate text-sm font-semibold" title={task.name}>
										{task.name}
									</div>
									<div class="text-muted-foreground font-mono text-xs">{task.id.slice(0, 12)}</div>
								</div>
								<StatusBadge text={task.currentState} variant={stateVariant(task.currentState)} />
							</div>
							<div class="grid grid-cols-2 gap-2">
								<div>
									<div class="text-muted-foreground mb-1 text-xs font-semibold">
										{m.swarm_node()}
									</div>
									<div class="flex items-center gap-1">
										<ConnectionIcon class="text-muted-foreground size-3" />
										<span class="text-foreground truncate text-sm">{task.nodeName || m.common_na()}</span>
									</div>
								</div>
								<div>
									<div class="text-muted-foreground mb-1 text-xs font-semibold">
										{m.swarm_desired_state()}
									</div>
									<StatusBadge text={task.desiredState} variant={stateVariant(task.desiredState)} size="sm" />
								</div>
								{#if task.error}
									<div class="col-span-2">
										<div class="text-muted-foreground mb-1 text-xs font-semibold">Error</div>
										<div class="text-sm break-all text-red-400">{task.error}</div>
									</div>
								{/if}
							</div>
						</Card.Content>
					</Card.Root>
				{/each}
			</div>
		{/if}
	</Card.Content>
</Card.Root>
