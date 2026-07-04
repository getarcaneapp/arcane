<script lang="ts">
	import StatusBadge from '$lib/components/badges/status-badge.svelte';
	import { m } from '$lib/paraglide/messages';
	import type { ContainerDetailsDto, ContainerHealthLogEntry, ContainerHealthcheckDto } from '$lib/types/docker';
	import { HealthIcon, SettingsIcon, FileTextIcon } from '$lib/icons';
	import DetailPanel from '$lib/components/resource-detail/detail-panel.svelte';
	import DetailSectionCard from '$lib/components/detail-section-card.svelte';
	import { formatDistanceToNow } from 'date-fns';
	import { formatDateTime } from '$lib/utils/formatting';

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

	function normalizeLog(entries: ContainerHealthLogEntry[] | undefined) {
		if (!entries) return [];
		return entries
			.map((e) => ({
				start: parseDockerDate(e.start),
				end: parseDockerDate(e.end),
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
		return formatDateTime(d) || d.toISOString();
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

{#snippet kv(label: string, value: string, mono = false)}
	<div>
		<div class="text-muted-foreground text-[11px] font-semibold tracking-wide uppercase">{label}</div>
		<div class={mono ? 'text-foreground mt-1 font-mono text-sm font-medium' : 'text-foreground mt-1 text-sm font-medium'}>
			{value}
		</div>
	</div>
{/snippet}

<DetailPanel>
	<DetailSectionCard icon={HealthIcon} title={m.common_health_status()} description={m.health_status_description()}>
		<div class="grid grid-cols-1 gap-x-6 gap-y-4 sm:grid-cols-2 lg:grid-cols-3 xl:grid-cols-4">
			<div>
				<div class="text-muted-foreground text-[11px] font-semibold tracking-wide uppercase">
					{m.common_health_status()}
				</div>
				<div class="mt-1">
					<StatusBadge variant={statusVariant} text={health?.status ?? m.common_unknown()} size="md" />
				</div>
			</div>

			{@render kv(m.health_failing_streak(), String(health?.failingStreak ?? 0))}

			{#if retriesBudget}
				{@render kv(m.health_retries_remaining(), `${retriesBudget.remaining} / ${retriesBudget.retries}`)}
			{/if}

			{#if nextCheck}
				<div>
					<div class="text-muted-foreground text-[11px] font-semibold tracking-wide uppercase">
						{m.health_next_check()}
					</div>
					<div class="text-foreground mt-1 text-sm font-medium" title={formatProbeDate(nextCheck.at)}>
						{#if nextCheck.overdue}
							{m.health_next_check_running_now()}
						{:else}
							{formatDistanceToNow(nextCheck.at, { addSuffix: true })}
						{/if}
					</div>
				</div>
			{/if}
		</div>
	</DetailSectionCard>

	<DetailSectionCard icon={SettingsIcon} title={m.health_configuration()} description={m.health_configuration_description()}>
		<div class="space-y-4">
			<div>
				<div class="text-muted-foreground text-[11px] font-semibold tracking-wide uppercase">
					{m.health_test_command()}
				</div>
				{#if testCommand.type === 'inherit'}
					<div class="text-muted-foreground mt-1 text-sm italic">
						{m.health_inherit_from_image()}
					</div>
				{:else if testCommand.type === 'none'}
					<div class="text-muted-foreground mt-1 text-sm italic">
						{m.health_disabled_in_image()}
					</div>
				{:else}
					<pre
						class="text-foreground mt-1 cursor-pointer rounded-md bg-black/5 p-2 font-mono text-sm break-all whitespace-pre-wrap select-all dark:bg-white/5"
						title={m.common_click_to_select()}>{testCommand.text}</pre>
				{/if}
			</div>

			<div class="grid grid-cols-1 gap-x-6 gap-y-4 sm:grid-cols-2 lg:grid-cols-3 xl:grid-cols-5">
				{@render kv(m.health_interval(), formatDurationNs(healthcheck?.interval), true)}
				{@render kv(m.health_timeout(), formatDurationNs(healthcheck?.timeout), true)}
				{@render kv(m.health_start_period(), formatDurationNs(healthcheck?.startPeriod), true)}
				{@render kv(m.health_start_interval(), formatDurationNs(healthcheck?.startInterval), true)}
				{@render kv(m.health_retries(), String(healthcheck?.retries ?? 0), true)}
			</div>
		</div>
	</DetailSectionCard>

	<DetailSectionCard icon={FileTextIcon} title={m.health_recent_probes()} description={m.health_recent_probes_description()}>
		{#if recentProbes.length === 0}
			<div class="text-muted-foreground rounded-lg border border-dashed py-8 text-center">
				<div class="text-sm">{m.health_no_probes_yet()}</div>
			</div>
		{:else}
			<div class="divide-border/50 divide-y">
				{#each recentProbes as probe (probeKey(probe))}
					{@const key = probeKey(probe)}
					<div class="flex flex-col gap-2 py-3 first:pt-0 last:pb-0">
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
					</div>
				{/each}
			</div>
		{/if}
	</DetailSectionCard>
</DetailPanel>
