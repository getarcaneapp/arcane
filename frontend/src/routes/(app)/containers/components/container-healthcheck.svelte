<script lang="ts">
	import { KeyValueCard } from '#lib/components/resource-detail/index.js';
	import { Badge } from '#lib/components/ui/badge/index.js';
	import * as Card from '#lib/components/ui/card/index.js';
	import { HealthIcon, SettingsIcon, FileTextIcon } from '#lib/icons/index.js';
	import { m } from '#lib/paraglide/messages.js';
	import type { ContainerDetailsDto, ContainerHealthLogEntry, ContainerHealthcheckDto } from '#lib/types/docker.js';
	import { formatDateTime, formatRelativeTime, parseInstant } from '#lib/utils/formatting.js';

	interface Props {
		container: ContainerDetailsDto;
	}

	let { container }: Props = $props();

	const healthcheck = $derived<ContainerHealthcheckDto | undefined>(container?.config?.healthcheck);
	const health = $derived(container?.state?.health);

	// Docker sends duration values in nanoseconds. Convert to a compact human string.
	function formatDurationNs(ns: number | undefined | null): string {
		if (!ns || ns <= 0) return m.common_unknown();
		const ms = Temporal.Duration.from({ nanoseconds: Math.trunc(ns) }).total('milliseconds');
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

	function normalizeLog(entries: ContainerHealthLogEntry[] | undefined) {
		if (!entries) return [];
		return entries
			.map((e) => ({
				start: parseInstant(e.start),
				end: parseInstant(e.end),
				exitCode: (e.exitCode ?? 0) as number,
				output: (e.output ?? '') as string
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
	const nextCheck = $derived.by<{ at: Temporal.Instant; overdue: boolean } | null>(() => {
		if (!container?.state?.running) return null;
		const intervalNs = healthcheck?.interval;
		if (!intervalNs || !lastProbe?.end) return null;
		const next = lastProbe.end.add({ nanoseconds: Math.trunc(intervalNs) });
		return { at: next, overdue: Temporal.Instant.compare(next, Temporal.Now.instant()) <= 0 };
	});

	function probeDuration(start: Temporal.Instant | null, end: Temporal.Instant | null): string {
		if (!start || !end) return '—';
		const ms = start.until(end, { largestUnit: 'millisecond' }).total('milliseconds');
		if (ms < 0) return '—';
		if (ms < 1000) return `${Math.round(ms)}ms`;
		return `${(ms / 1000).toFixed(2)}s`;
	}

	function formatProbeDate(instant: Temporal.Instant | null): string {
		if (!instant) return '—';
		return formatDateTime(instant) || instant.toString({ smallestUnit: 'millisecond' });
	}

	const retriesBudget = $derived.by(() => {
		const retries = healthcheck?.retries;
		const failing = health?.failingStreak ?? 0;
		if (retries === undefined || retries === null) return null;
		return { retries, failing, remaining: Math.max(0, retries - failing) };
	});

	function probeKey(probe: { start: Temporal.Instant | null; end: Temporal.Instant | null; exitCode: number }): string {
		return `${probe.start?.epochNanoseconds ?? ''}-${probe.end?.epochNanoseconds ?? ''}-${probe.exitCode}`;
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
		<Card.Content>
			<div class="grid grid-cols-1 gap-3 sm:grid-cols-2 lg:grid-cols-3 xl:grid-cols-4">
				<KeyValueCard label={m.common_health_status()} valueFormat="custom" stacked
					><div>
						<Badge variant={statusVariant} minWidth="20">{health?.status ?? m.common_unknown()}</Badge>
					</div></KeyValueCard
				>

				<KeyValueCard label={m.health_failing_streak()} valueFormat="custom" stacked
					><div class="text-sm font-medium text-foreground">
						{health?.failingStreak ?? 0}
					</div></KeyValueCard
				>

				{#if retriesBudget}
					<KeyValueCard label={m.health_retries_remaining()} valueFormat="custom" stacked
						><div class="text-sm font-medium text-foreground">
							{retriesBudget.remaining} / {retriesBudget.retries}
						</div></KeyValueCard
					>
				{/if}

				{#if nextCheck}
					<KeyValueCard label={m.health_next_check()} valueFormat="custom" stacked
						><div class="text-sm font-medium text-foreground" title={formatProbeDate(nextCheck.at)}>
							{#if nextCheck.overdue}
								{m.health_next_check_running_now()}
							{:else}
								{formatRelativeTime(nextCheck.at)}
							{/if}
						</div></KeyValueCard
					>
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
		<Card.Content>
			<div class="space-y-3">
				<KeyValueCard label={m.health_test_command()} valueFormat="custom" stacked
					>{#if testCommand.type === 'inherit'}
						<div class="text-sm text-muted-foreground italic">
							{m.health_inherit_from_image()}
						</div>
					{:else if testCommand.type === 'none'}
						<div class="text-sm text-muted-foreground italic">
							{m.health_disabled_in_image()}
						</div>
					{:else}
						<pre
							class="cursor-pointer rounded-md bg-black/5 p-2 font-mono text-sm break-all whitespace-pre-wrap text-foreground select-all dark:bg-white/5"
							title={m.common_click_to_select()}>{testCommand.text}</pre>
					{/if}</KeyValueCard
				>

				<div class="grid grid-cols-1 gap-3 sm:grid-cols-2 lg:grid-cols-3 xl:grid-cols-5">
					<KeyValueCard label={m.health_interval()} valueFormat="custom" stacked
						><div class="font-mono text-sm font-medium text-foreground">
							{formatDurationNs(healthcheck?.interval)}
						</div></KeyValueCard
					>
					<KeyValueCard label={m.health_timeout()} valueFormat="custom" stacked
						><div class="font-mono text-sm font-medium text-foreground">
							{formatDurationNs(healthcheck?.timeout)}
						</div></KeyValueCard
					>
					<KeyValueCard label={m.health_start_period()} valueFormat="custom" stacked
						><div class="font-mono text-sm font-medium text-foreground">
							{formatDurationNs(healthcheck?.startPeriod)}
						</div></KeyValueCard
					>
					<KeyValueCard label={m.health_start_interval()} valueFormat="custom" stacked
						><div class="font-mono text-sm font-medium text-foreground">
							{formatDurationNs(healthcheck?.startInterval)}
						</div></KeyValueCard
					>
					<KeyValueCard label={m.health_retries()} valueFormat="custom" stacked
						><div class="font-mono text-sm font-medium text-foreground">
							{healthcheck?.retries ?? 0}
						</div></KeyValueCard
					>
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
		<Card.Content>
			{#if recentProbes.length === 0}
				<div class="rounded-lg border border-dashed py-8 text-center text-muted-foreground">
					<div class="text-sm">{m.health_no_probes_yet()}</div>
				</div>
			{:else}
				<div class="space-y-2">
					{#each recentProbes as probe (probeKey(probe))}
						{@const key = probeKey(probe)}
						<Card.Root variant="subtle">
							<Card.Content>
								<div class="flex flex-col gap-2">
									<div class="flex flex-wrap items-center justify-between gap-2">
										<div class="flex items-center gap-3">
											<Badge variant={probe.exitCode === 0 ? 'green' : 'red'} size="sm" minWidth="20"
												>{`${m.health_exit_code()}: ${probe.exitCode}`}</Badge
											>
											<span class="text-xs text-muted-foreground" title={formatProbeDate(probe.start)}>
												{probe.start ? formatRelativeTime(probe.start) : '—'}
											</span>
											<span class="text-xs text-muted-foreground">
												{m.duration()}: {probeDuration(probe.start, probe.end)}
											</span>
										</div>
										{#if probe.output}
											<button type="button" class="text-xs text-primary hover:underline" onclick={() => toggleExpanded(key)}>
												{expanded[key] ? m.common_hide() : m.common_show()}
											</button>
										{/if}
									</div>
									{#if probe.output && expanded[key]}
										<pre
											class="max-h-64 overflow-auto rounded-md bg-black/5 p-2 font-mono text-xs whitespace-pre-wrap text-foreground dark:bg-white/5">{probe.output}</pre>
									{/if}
								</div>
							</Card.Content>
						</Card.Root>
					{/each}
				</div>
			{/if}
		</Card.Content>
	</Card.Root>
</div>
