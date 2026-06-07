<script lang="ts">
	import * as Card from '$lib/components/ui/card';
	import StatusBadge from '$lib/components/badges/status-badge.svelte';
	import { m } from '$lib/paraglide/messages';
	import type { ContainerDetailsDto, ContainerHealthLogEntry, ContainerHealthcheckDto } from '$lib/types/container.type';
	import { HealthIcon, SettingsIcon, FileTextIcon } from '$lib/icons';
	import { format, formatDistanceToNow } from 'date-fns';

	interface Props {
		container: ContainerDetailsDto;
	}

	let { container }: Props = $props();

	const healthcheck = $derived<ContainerHealthcheckDto | undefined>(container?.config?.healthcheck);
	const health = $derived(container?.state?.health);

	// Docker sends duration values in nanoseconds. Convert to a compact human string.
	function formatDurationNs(ns: number | undefined | null): string {
		if (!ns || ns <= 0) return m.common_unknown();
		const ms = ns / 1_000_000;
		if (ms < 1000) return `${Math.round(ms)}ms`;
		const totalSeconds = Math.round(ms / 1000);
		if (totalSeconds < 60) return `${totalSeconds}s`;
		const minutes = Math.floor(totalSeconds / 60);
		const seconds = totalSeconds % 60;
		if (minutes < 60) return seconds ? `${minutes}m ${seconds}s` : `${minutes}m`;
		const hours = Math.floor(minutes / 60);
		const mins = minutes % 60;
		return mins ? `${hours}h ${mins}m` : `${hours}h`;
	}

	function parseDockerDate(input: string | undefined | null): Date | null {
		if (!input) return null;
		const s = String(input).trim();
		if (!s || s.startsWith('0001-01-01')) return null;
		const d = new Date(s);
		return isNaN(d.getTime()) ? null : d;
	}

	// Normalize log entry casing (backend now emits lowercase; tolerate older PascalCase).
	function normalizeLog(entries: ContainerHealthLogEntry[] | undefined) {
		if (!entries) return [];
		return entries
			.map((e) => ({
				start: parseDockerDate(e.start ?? e.Start),
				end: parseDockerDate(e.end ?? e.End),
				exitCode: (e.exitCode ?? e.ExitCode ?? 0) as number,
				output: (e.output ?? e.Output ?? '') as string
			}))
			.filter((e) => e.start || e.end);
	}

	const logs = $derived(normalizeLog(health?.log));

	// Reverse — most recent first for display.
	const recentProbes = $derived([...logs].reverse());

	const lastProbe = $derived(logs.length > 0 ? logs[logs.length - 1] : null);

	const statusVariant = $derived.by<'green' | 'red' | 'amber' | 'gray'>(() => {
		const s = health?.status?.toLowerCase();
		if (s === 'healthy') return 'green';
		if (s === 'unhealthy') return 'red';
		if (s === 'starting') return 'amber';
		return 'gray';
	});

	const testCommand = $derived.by<{ type: 'none' | 'inherit' | 'cmd'; text: string }>(() => {
		const test = healthcheck?.test;
		if (!test || test.length === 0) return { type: 'inherit', text: '' };
		if (test.length === 1 && test[0] === 'NONE') return { type: 'none', text: '' };
		// First element is typically "CMD" or "CMD-SHELL".
		const [head, ...rest] = test;
		if (head === 'CMD-SHELL') return { type: 'cmd', text: rest.join(' ') };
		if (head === 'CMD') return { type: 'cmd', text: rest.join(' ') };
		return { type: 'cmd', text: test.join(' ') };
	});

	// Estimate the next probe time: lastProbe.end + interval (clamped to "now" if overdue).
	const nextCheck = $derived.by<{ at: Date; overdue: boolean } | null>(() => {
		if (!container?.state?.running) return null;
		const intervalNs = healthcheck?.interval;
		if (!intervalNs || !lastProbe?.end) return null;
		const next = new Date(lastProbe.end.getTime() + intervalNs / 1_000_000);
		const now = new Date();
		return { at: next, overdue: next.getTime() <= now.getTime() };
	});

	function probeDuration(start: Date | null, end: Date | null): string {
		if (!start || !end) return '—';
		const ms = end.getTime() - start.getTime();
		if (ms < 0) return '—';
		if (ms < 1000) return `${ms}ms`;
		return `${(ms / 1000).toFixed(2)}s`;
	}

	function formatProbeDate(d: Date | null): string {
		if (!d) return '—';
		try {
			return format(d, 'PPpp');
		} catch {
			return d.toISOString();
		}
	}

	const retriesBudget = $derived.by(() => {
		const retries = healthcheck?.retries;
		const failing = health?.failingStreak ?? 0;
		if (retries === undefined || retries === null) return null;
		return { retries, failing, remaining: Math.max(0, retries - failing) };
	});

	function probeKey(probe: { start: Date | null; end: Date | null; exitCode: number }): string {
		return `${probe.start?.getTime() ?? ''}-${probe.end?.getTime() ?? ''}-${probe.exitCode}`;
	}

	let expanded = $state<Record<string, boolean>>({});
	function toggleExpanded(key: string) {
		expanded[key] = !expanded[key];
	}
</script>

<div class="space-y-6">
	<Card.Root>
		<Card.Header icon={HealthIcon}>
			<div class="flex flex-col space-y-1.5">
				<Card.Title>
					<h2>{m.common_health_status()}</h2>
				</Card.Title>
				<Card.Description>{m.health_status_description()}</Card.Description>
			</div>
		</Card.Header>
		<Card.Content class="p-4">
			<div class="grid grid-cols-1 gap-3 sm:grid-cols-2 lg:grid-cols-3 xl:grid-cols-4">
				<Card.Root variant="subtle">
					<Card.Content class="flex flex-col gap-2 p-4">
						<div class="text-muted-foreground text-xs font-semibold tracking-wide uppercase">
							{m.common_health_status()}
						</div>
						<div>
							<StatusBadge variant={statusVariant} text={health?.status ?? m.common_unknown()} size="md" />
						</div>
					</Card.Content>
				</Card.Root>

				<Card.Root variant="subtle">
					<Card.Content class="flex flex-col gap-2 p-4">
						<div class="text-muted-foreground text-xs font-semibold tracking-wide uppercase">
							{m.health_failing_streak()}
						</div>
						<div class="text-foreground text-sm font-medium">
							{health?.failingStreak ?? 0}
						</div>
					</Card.Content>
				</Card.Root>

				{#if retriesBudget}
					<Card.Root variant="subtle">
						<Card.Content class="flex flex-col gap-2 p-4">
							<div class="text-muted-foreground text-xs font-semibold tracking-wide uppercase">
								{m.health_retries_remaining()}
							</div>
							<div class="text-foreground text-sm font-medium">
								{retriesBudget.remaining} / {retriesBudget.retries}
							</div>
						</Card.Content>
					</Card.Root>
				{/if}

				{#if nextCheck}
					<Card.Root variant="subtle">
						<Card.Content class="flex flex-col gap-2 p-4">
							<div class="text-muted-foreground text-xs font-semibold tracking-wide uppercase">
								{m.health_next_check()}
							</div>
							<div class="text-foreground text-sm font-medium" title={formatProbeDate(nextCheck.at)}>
								{#if nextCheck.overdue}
									{m.health_next_check_running_now()}
								{:else}
									{formatDistanceToNow(nextCheck.at, { addSuffix: true })}
								{/if}
							</div>
						</Card.Content>
					</Card.Root>
				{/if}
			</div>
		</Card.Content>
	</Card.Root>

	<Card.Root>
		<Card.Header icon={SettingsIcon}>
			<div class="flex flex-col space-y-1.5">
				<Card.Title>
					<h2>{m.health_configuration()}</h2>
				</Card.Title>
				<Card.Description>{m.health_configuration_description()}</Card.Description>
			</div>
		</Card.Header>
		<Card.Content class="p-4">
			<div class="space-y-3">
				<Card.Root variant="subtle">
					<Card.Content class="flex flex-col gap-2 p-4">
						<div class="text-muted-foreground text-xs font-semibold tracking-wide uppercase">
							{m.health_test_command()}
						</div>
						{#if testCommand.type === 'inherit'}
							<div class="text-muted-foreground text-sm italic">
								{m.health_inherit_from_image()}
							</div>
						{:else if testCommand.type === 'none'}
							<div class="text-muted-foreground text-sm italic">
								{m.health_disabled_in_image()}
							</div>
						{:else}
							<pre
								class="text-foreground cursor-pointer rounded-md bg-black/5 p-2 font-mono text-sm break-all whitespace-pre-wrap select-all dark:bg-white/5"
								title="Click to select">{testCommand.text}</pre>
						{/if}
					</Card.Content>
				</Card.Root>

				<div class="grid grid-cols-1 gap-3 sm:grid-cols-2 lg:grid-cols-3 xl:grid-cols-5">
					<Card.Root variant="subtle">
						<Card.Content class="flex flex-col gap-2 p-4">
							<div class="text-muted-foreground text-xs font-semibold tracking-wide uppercase">
								{m.health_interval()}
							</div>
							<div class="text-foreground font-mono text-sm font-medium">
								{formatDurationNs(healthcheck?.interval)}
							</div>
						</Card.Content>
					</Card.Root>
					<Card.Root variant="subtle">
						<Card.Content class="flex flex-col gap-2 p-4">
							<div class="text-muted-foreground text-xs font-semibold tracking-wide uppercase">
								{m.health_timeout()}
							</div>
							<div class="text-foreground font-mono text-sm font-medium">
								{formatDurationNs(healthcheck?.timeout)}
							</div>
						</Card.Content>
					</Card.Root>
					<Card.Root variant="subtle">
						<Card.Content class="flex flex-col gap-2 p-4">
							<div class="text-muted-foreground text-xs font-semibold tracking-wide uppercase">
								{m.health_start_period()}
							</div>
							<div class="text-foreground font-mono text-sm font-medium">
								{formatDurationNs(healthcheck?.startPeriod)}
							</div>
						</Card.Content>
					</Card.Root>
					<Card.Root variant="subtle">
						<Card.Content class="flex flex-col gap-2 p-4">
							<div class="text-muted-foreground text-xs font-semibold tracking-wide uppercase">
								{m.health_start_interval()}
							</div>
							<div class="text-foreground font-mono text-sm font-medium">
								{formatDurationNs(healthcheck?.startInterval)}
							</div>
						</Card.Content>
					</Card.Root>
					<Card.Root variant="subtle">
						<Card.Content class="flex flex-col gap-2 p-4">
							<div class="text-muted-foreground text-xs font-semibold tracking-wide uppercase">
								{m.health_retries()}
							</div>
							<div class="text-foreground font-mono text-sm font-medium">
								{healthcheck?.retries ?? 0}
							</div>
						</Card.Content>
					</Card.Root>
				</div>
			</div>
		</Card.Content>
	</Card.Root>

	<Card.Root>
		<Card.Header icon={FileTextIcon}>
			<div class="flex flex-col space-y-1.5">
				<Card.Title>
					<h2>{m.health_recent_probes()}</h2>
				</Card.Title>
				<Card.Description>{m.health_recent_probes_description()}</Card.Description>
			</div>
		</Card.Header>
		<Card.Content class="p-4">
			{#if recentProbes.length === 0}
				<div class="text-muted-foreground rounded-lg border border-dashed py-8 text-center">
					<div class="text-sm">{m.health_no_probes_yet()}</div>
				</div>
			{:else}
				<div class="space-y-2">
					{#each recentProbes as probe (probeKey(probe))}
						{@const key = probeKey(probe)}
						<Card.Root variant="subtle">
							<Card.Content class="flex flex-col gap-2 p-4">
								<div class="flex flex-wrap items-center justify-between gap-2">
									<div class="flex items-center gap-3">
										<StatusBadge
											variant={probe.exitCode === 0 ? 'green' : 'red'}
											text={`${m.health_exit_code()}: ${probe.exitCode}`}
											size="sm"
										/>
										<span class="text-muted-foreground text-xs" title={formatProbeDate(probe.start)}>
											{probe.start ? formatDistanceToNow(probe.start, { addSuffix: true }) : '—'}
										</span>
										<span class="text-muted-foreground text-xs">
											{m.health_probe_duration()}: {probeDuration(probe.start, probe.end)}
										</span>
									</div>
									{#if probe.output}
										<button type="button" class="text-primary text-xs hover:underline" onclick={() => toggleExpanded(key)}>
											{expanded[key] ? m.common_hide() : m.common_show()}
										</button>
									{/if}
								</div>
								{#if probe.output && expanded[key]}
									<pre
										class="text-foreground max-h-64 overflow-auto rounded-md bg-black/5 p-2 font-mono text-xs whitespace-pre-wrap dark:bg-white/5">{probe.output}</pre>
								{/if}
							</Card.Content>
						</Card.Root>
					{/each}
				</div>
			{/if}
		</Card.Content>
	</Card.Root>
</div>
