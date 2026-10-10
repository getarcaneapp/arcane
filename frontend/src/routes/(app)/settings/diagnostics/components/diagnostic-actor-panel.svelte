<script lang="ts">
	import { onDestroy, onMount } from 'svelte';
	import { SvelteSet } from 'svelte/reactivity';
	import { slide } from 'svelte/transition';

	import { jobStatusLabel } from '#lib/components/job-card/job-status.js';
	import * as Alert from '#lib/components/ui/alert/index.js';
	import * as Table from '#lib/components/ui/table/index.js';
	import { ArrowRightIcon } from '#lib/icons/index.js';
	import { m } from '#lib/paraglide/messages.js';
	import type { ActorDiagnostics, ActorTypeInfo } from '#lib/types/diagnostics.js';
	import { cn } from '#lib/utils.js';
	import { formatTime } from '#lib/utils/formatting.js';
	import { createActorDiagnosticsWebSocket, ReconnectingWebSocket } from '#lib/utils/ws.js';

	let environments = $state<ActorDiagnostics[]>([]);
	const expanded = new SvelteSet<string>();
	let ws: ReconnectingWebSocket<ActorDiagnostics[]> | null = null;

	function toggle(key: string) {
		if (expanded.has(key)) expanded.delete(key);
		else expanded.add(key);
	}

	function hasDetails(t: ActorTypeInfo): boolean {
		return Boolean(t.instances?.length || t.recentFailures?.length);
	}

	onMount(() => {
		ws = createActorDiagnosticsWebSocket({ onMessage: (data) => (environments = data) });
		ws.connect();
	});

	onDestroy(() => ws?.close());
</script>

<div class="space-y-4">
	{#each environments as env (env.environmentId)}
		<div class="space-y-3 rounded-xl border border-border/60 p-4">
			<div class="flex flex-wrap items-center gap-2">
				<h3 class="text-sm font-semibold">{env.environmentName || env.environmentId}</h3>
				{#if !env.error && !env.ready}
					<span class="text-xs text-warning">{m.jobs_status_starting()}</span>
				{/if}
				{#each env.hosts ?? [] as host (host.hostId)}
					<span class="font-mono text-xs text-muted-foreground">
						{host.address} · {m.last_check()}
						{formatTime(host.lastHealthCheck) || '—'}
					</span>
				{/each}
			</div>

			{#if env.error}
				<Alert.Root variant="destructive">
					<Alert.Title>{m.common_unavailable()}</Alert.Title>
					<Alert.Description>{env.error}</Alert.Description>
				</Alert.Root>
			{:else if env.ready}
				<div class="flex flex-wrap items-center gap-x-4 gap-y-1 text-xs">
					<span class="text-muted-foreground">{m.jobs_run_history()}</span>
					{#each Object.entries(env.runs ?? {}) as [status, count] (status)}
						<span><span class="font-semibold tabular-nums">{count}</span> {jobStatusLabel(status, true)}</span>
					{:else}
						<span class="text-muted-foreground">{m.none()}</span>
					{/each}
				</div>

				{#if env.actorTypes?.length}
					<div class="overflow-x-auto">
						<Table.Root>
							<Table.Header>
								<Table.Row variant="static">
									<Table.Head variant="expander"></Table.Head>
									<Table.Head>{m.common_type()}</Table.Head>
									<Table.Head>{m.common_active()}</Table.Head>
									<Table.Head>{m.common_state()}</Table.Head>
									<Table.Head>{m.diagnostics_actors_alarms()}</Table.Head>
									<Table.Head>{m.common_pending()}</Table.Head>
									<Table.Head>{m.common_running()}</Table.Head>
									<Table.Head>{m.completed()}</Table.Head>
									<Table.Head>{m.common_failed()}</Table.Head>
									<Table.Head>{m.jobs_next_run()}</Table.Head>
								</Table.Row>
							</Table.Header>
							<Table.Body>
								{#each env.actorTypes as t (t.actorType)}
									{@const key = `${env.environmentId}:${t.actorType}`}
									{@const expandable = hasDetails(t)}
									{@const open = expandable && expanded.has(key)}
									<Table.Row
										variant={expandable ? 'default' : 'static'}
										data-expanded={open ? true : undefined}
										class={cn(!expandable && 'cursor-default')}
										onclick={() => expandable && toggle(key)}
									>
										<Table.Cell variant="expander">
											{#if expandable}
												<ArrowRightIcon
													class={cn('size-4 text-muted-foreground transition-transform duration-200', open && 'rotate-90')}
													aria-hidden="true"
												/>
											{/if}
										</Table.Cell>
										<Table.Cell><span class="font-mono text-xs">{t.actorType}</span></Table.Cell>
										<Table.Cell>
											<span class="tabular-nums">
												{t.activeActors}{#if t.concurrencyLimit > 0}<span class="text-muted-foreground">
														/ {m.common_limit()} {t.concurrencyLimit}</span
													>{/if}
											</span>
										</Table.Cell>
										{#each [t.storedStates, t.pendingAlarms, t.pendingJobs, t.leasedAlarms, t.completedJobs] as value, i (i)}
											<Table.Cell><span class="tabular-nums">{value}</span></Table.Cell>
										{/each}
										<Table.Cell>
											<span class={cn('tabular-nums', t.deadJobs > 0 && 'text-destructive')}>{t.deadJobs}</span>
										</Table.Cell>
										<Table.Cell>
											<span class="text-xs text-muted-foreground tabular-nums">
												{t.nextAlarmAt ? formatTime(t.nextAlarmAt) : '—'}
											</span>
										</Table.Cell>
									</Table.Row>
									{#if open}
										<Table.Row variant="detail">
											<Table.Cell colspan={10} variant="flush">
												<div transition:slide={{ duration: 200 }} class="space-y-4 px-6 py-4">
													{#if t.instances?.length}
														{@render instanceList(t)}
													{/if}
													{#if t.recentFailures?.length}
														{@render failureList(t)}
													{/if}
												</div>
											</Table.Cell>
										</Table.Row>
									{/if}
								{/each}
							</Table.Body>
						</Table.Root>
					</div>
				{:else}
					<p class="text-sm text-muted-foreground">{m.none()}</p>
				{/if}
			{/if}
		</div>
	{:else}
		<p class="py-8 text-sm text-muted-foreground">{m.diagnostics_status_connecting()}</p>
	{/each}
</div>

{#snippet instanceList(t: ActorTypeInfo)}
	<section class="space-y-1.5">
		<h4 class="text-2xs font-semibold tracking-wide text-muted-foreground uppercase">
			{m.common_active()} · {t.activeActors}
		</h4>
		<ul class="ml-1 border-l border-border/40">
			{#each t.instances as instance (instance.actorId)}
				<li class="flex items-baseline gap-3 py-1 pl-3 text-xs">
					<span class="font-medium">{instance.name || instance.actorId.slice(0, 12)}</span>
					<span class="min-w-0 flex-1 truncate font-mono text-2xs text-muted-foreground" title={instance.actorId}
						>{instance.actorId}</span
					>
					<span class="shrink-0 text-2xs text-muted-foreground tabular-nums">
						{m.common_started()}
						<span class="text-foreground">{formatTime(instance.activatedAt)}</span>
					</span>
				</li>
			{/each}
		</ul>
	</section>
{/snippet}

{#snippet failureList(t: ActorTypeInfo)}
	<section class="space-y-1.5">
		<h4 class="text-2xs font-semibold tracking-wide text-destructive uppercase">
			{m.common_failed()} · {t.deadJobs}
		</h4>
		<ul class="ml-1 border-l border-destructive/40">
			{#each t.recentFailures as failure (failure.jobId)}
				<li class="space-y-1 py-1.5 pl-3 text-xs">
					<div class="flex items-baseline gap-3">
						<span class="font-medium">{failure.name || failure.actorId.slice(0, 12)}</span>
						<span class="font-mono text-2xs text-muted-foreground">{failure.method}</span>
						<span class="ml-auto shrink-0 text-2xs text-muted-foreground tabular-nums">
							{m.jobs_run_attempts()} <span class="text-foreground">{failure.attempts}</span> ·
							<span class="text-foreground">{formatTime(failure.failedAt)}</span>
						</span>
					</div>
					{#if failure.lastError}
						<p
							class="rounded-md border border-destructive/30 bg-destructive/10 px-2 py-1 font-mono text-2xs break-words text-destructive"
						>
							{failure.lastError}
						</p>
					{/if}
				</li>
			{/each}
		</ul>
	</section>
{/snippet}
