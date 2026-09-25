<script lang="ts">
	import * as ArcaneTooltip from '#lib/components/arcane-tooltip/index.js';
	import { Badge } from '#lib/components/ui/badge/index.js';
	import RiskGauge from '#lib/components/vulnerability/risk-gauge.svelte';
	import RiskTrendChart from '#lib/components/vulnerability/risk-trend-chart.svelte';
	import ShareBar from '#lib/components/vulnerability/share-bar.svelte';
	import { ArrowDownIcon, ArrowUpIcon, InfoIcon, ScanIcon } from '#lib/icons/index.js';
	import { m } from '#lib/paraglide/messages.js';
	import type { VulnerabilityRiskDrivers, VulnerabilityRiskImage, VulnerabilityRiskOverview } from '#lib/types/environment.js';
	import { formatRelativeTime } from '#lib/utils/formatting.js';
	import {
		getRiskBandDisplay,
		getSeverityBarClass,
		getSeverityLabel,
		getSeveritySegments,
		type ShareBarSegment
	} from '#lib/utils/vulnerability.js';

	interface Props {
		overview: VulnerabilityRiskOverview;
		onSelectVulnerability: (vulnerabilityId: string) => void;
	}

	let { overview, onSelectVulnerability }: Props = $props();

	let trendDays = $state('30');
	let findingRank = $state<'risk' | 'prevalence'>('risk');

	const trendPoints = $derived(overview.trend.slice(-Number(trendDays)));
	const topFindings = $derived(findingRank === 'risk' ? overview.riskiestFindings : overview.prevalentFindings);
	const severitySegments = $derived(getSeveritySegments(overview.summary));
	// One hue stepped by exposure: running findings carry full weight, unused the least.
	const exposureSegments = $derived<ShareBarSegment[]>([
		{ key: 'running', label: m.security_exposure_running(), count: overview.exposure.running, barClass: 'bg-primary' },
		{ key: 'stopped', label: m.security_image_stopped(), count: overview.exposure.stopped, barClass: 'bg-primary/50' },
		{ key: 'unused', label: m.security_image_unused(), count: overview.exposure.unused, barClass: 'bg-muted-foreground/40' },
		{ key: 'unknown', label: m.common_unknown(), count: overview.exposure.unknown, barClass: 'bg-muted-foreground/20' }
	]);

	const intelNote = $derived.by(() => {
		const intel = overview.threatIntel;
		if (!intel.enabled) return m.security_threat_intel_disabled();
		if (!intel.lastSyncedAt) return m.security_threat_intel_pending();
		if (intel.stale) return m.security_threat_intel_stale({ time: formatRelativeTime(intel.lastSyncedAt) });
		return m.security_threat_intel_updated({ time: formatRelativeTime(intel.lastSyncedAt) });
	});

	type Driver = {
		key: keyof VulnerabilityRiskDrivers;
		label: string;
		help: string;
		// Worsening drivers show a week-over-week delta; the rest show their denominator.
		trackDelta: boolean;
		context?: string;
	};

	const drivers = $derived<Driver[]>([
		{
			key: 'knownExploited',
			label: m.security_driver_known_exploited(),
			help: m.security_driver_known_exploited_help(),
			trackDelta: true
		},
		{ key: 'highEpss', label: m.security_driver_high_epss(), help: m.security_driver_high_epss_help(), trackDelta: true },
		{
			key: 'exposedCriticalHigh',
			label: m.security_driver_exposed(),
			help: m.security_driver_exposed_help(),
			trackDelta: true
		},
		{
			key: 'overdueKnownExploited',
			label: m.security_driver_overdue_kev(),
			help: m.security_driver_overdue_kev_help(),
			trackDelta: true
		},
		{
			key: 'imagesScanned',
			label: m.security_images_scanned(),
			help: m.security_driver_images_scanned_help(),
			trackDelta: false,
			context: m.security_driver_of_images({ count: overview.drivers.imagesTotal })
		}
	]);

	function driverDelta(key: keyof VulnerabilityRiskDrivers): number | null {
		const previous = overview.drivers7dAgo;
		if (!previous) return null;
		return overview.drivers[key] - previous[key];
	}

	function imageUsageLabel(image: VulnerabilityRiskImage): string {
		switch (image.exposure) {
			case 'running':
				return m.security_running_containers({ count: image.runningContainers });
			case 'stopped':
				return m.security_image_stopped();
			case 'unused':
				return m.security_image_unused();
			default:
				return m.common_unknown();
		}
	}
</script>

{#snippet Delta(value: number | null | undefined)}
	{#if value !== null && value !== undefined && value !== 0}
		<span class="inline-flex items-center gap-0.5 text-xs text-muted-foreground tabular-nums">
			{#if value > 0}
				<ArrowUpIcon class="size-3.5 text-destructive" aria-hidden="true" />
			{:else}
				<ArrowDownIcon class="size-3.5 text-success" aria-hidden="true" />
			{/if}
			{Math.abs(value)}
		</span>
	{/if}
{/snippet}

{#snippet SegmentToggle(
	label: string,
	options: { value: string; label: string }[],
	current: string,
	onSelect: (value: string) => void
)}
	<div class="flex gap-1 text-xs" role="group" aria-label={label}>
		{#each options as option (option.value)}
			<button
				type="button"
				class="rounded-md px-2 py-1 transition-colors {current === option.value
					? 'bg-muted font-medium text-foreground'
					: 'text-muted-foreground hover:text-foreground'}"
				aria-pressed={current === option.value}
				onclick={() => onSelect(option.value)}
			>
				{option.label}
			</button>
		{/each}
	</div>
{/snippet}

{#snippet HelpTip(title: string, body: string)}
	<ArcaneTooltip.Root>
		<ArcaneTooltip.Trigger>
			<span class="inline-flex cursor-help items-center text-muted-foreground" aria-label={title}>
				<InfoIcon class="size-3.5 shrink-0" />
			</span>
		</ArcaneTooltip.Trigger>
		<ArcaneTooltip.Content class="max-w-64">
			<p class="mb-1 text-sm font-medium">{title}</p>
			<p class="text-xs text-muted-foreground">{body}</p>
		</ArcaneTooltip.Content>
	</ArcaneTooltip.Root>
{/snippet}

{#if overview.drivers.imagesScanned === 0}
	<div class="flex flex-col items-center gap-3 py-16 text-center">
		<ScanIcon class="size-10 text-muted-foreground/50" />
		<p class="text-sm text-muted-foreground">{m.security_overview_empty()}</p>
	</div>
{:else}
	<div class="space-y-12 py-6">
		<section class="grid items-center gap-10 lg:grid-cols-4 lg:gap-12">
			<div
				class="flex flex-col items-center gap-3 lg:col-span-2 lg:justify-center lg:self-stretch lg:border-r lg:border-border/50 lg:pr-12"
			>
				<RiskGauge score={overview.riskScore} band={overview.riskBand} size={460} stroke={22} />
				{#if overview.scoreDriver}
					{@const driver = overview.scoreDriver}
					<button
						type="button"
						class="flex max-w-sm flex-col items-center gap-1.5 rounded-md px-2 py-1 text-center text-sm text-muted-foreground transition-colors hover:bg-muted/40 hover:text-foreground"
						onclick={() => onSelectVulnerability(driver.vulnerabilityId)}
					>
						<span class="flex flex-wrap items-center justify-center gap-1.5">
							{m.security_score_driver({ cve: driver.vulnerabilityId, image: driver.imageName })}
							{#if driver.knownExploited}
								<Badge variant="red" size="xs">{m.vuln_kev_badge()}</Badge>
							{/if}
						</span>
						{#if driver.exposure === 'unused'}
							<span>{m.vuln_exposure_unused()}</span>
						{:else if driver.exposure === 'stopped'}
							<span>{m.vuln_exposure_stopped()}</span>
						{/if}
					</button>
				{/if}
			</div>
			<div class="min-w-0 space-y-8 lg:col-span-2">
				<div class="flex flex-wrap items-end justify-between gap-3">
					<div class="space-y-1">
						<h3 class="text-sm font-medium">{m.security_risk_trend()}</h3>
						{#if overview.delta7d !== undefined}
							<p class="flex items-center gap-1.5 text-xs text-muted-foreground">
								{#if overview.delta7d === 0}
									{m.security_risk_delta_unchanged()}
								{:else}
									{@render Delta(overview.delta7d)}
									{m.security_risk_delta_7d()}
								{/if}
							</p>
						{/if}
					</div>
					{@render SegmentToggle(
						m.security_risk_trend(),
						[
							{ value: '30', label: m.security_risk_range_30d() },
							{ value: '90', label: m.security_risk_range_90d() }
						],
						trendDays,
						(value) => (trendDays = value)
					)}
				</div>
				{#if trendPoints.length > 1}
					<RiskTrendChart points={trendPoints} days={Number(trendDays)} />
				{:else}
					<p class="py-6 text-center text-sm text-muted-foreground">{m.security_risk_trend_empty()}</p>
				{/if}
				<div class="grid gap-8 sm:grid-cols-2">
					<div class="min-w-0">
						<h4 class="mb-3 text-sm text-muted-foreground">{m.security_severity_breakdown()}</h4>
						<ShareBar segments={severitySegments} />
					</div>
					<div class="min-w-0">
						<h4 class="mb-3 flex items-center gap-1.5 text-sm text-muted-foreground">
							{m.security_exposure_breakdown()}
							{@render HelpTip(m.security_exposure_breakdown(), m.security_exposure_breakdown_help())}
						</h4>
						<ShareBar segments={exposureSegments} />
					</div>
				</div>
				<p class="text-xs text-muted-foreground">{intelNote}</p>
			</div>
		</section>

		<dl
			class="grid grid-cols-2 gap-y-6 border-y border-border/50 py-6 sm:grid-cols-3 lg:grid-cols-5 lg:divide-x lg:divide-border/50"
		>
			{#each drivers as driver (driver.key)}
				<div class="flex flex-col items-center px-4 text-center">
					<dt class="flex items-center justify-center gap-1.5 text-sm text-muted-foreground">
						{driver.label}
						{@render HelpTip(driver.label, driver.help)}
					</dt>
					<dd class="mt-2 flex items-baseline justify-center gap-2">
						<span class="text-3xl font-semibold tabular-nums">{overview.drivers[driver.key]}</span>
						{#if driver.trackDelta}
							{@render Delta(driverDelta(driver.key))}
						{:else if driver.context}
							<span class="text-xs text-muted-foreground tabular-nums">{driver.context}</span>
						{/if}
					</dd>
				</div>
			{/each}
		</dl>

		<div class="grid gap-12 lg:grid-cols-2">
			<section class="min-w-0">
				<div class="mb-3 flex flex-wrap items-center justify-between gap-3">
					<h3 class="flex items-center gap-1.5 text-sm font-medium">
						{m.security_top_vulnerabilities()}
						{@render HelpTip(
							m.security_top_vulnerabilities(),
							findingRank === 'risk'
								? m.security_top_vulnerabilities_risk_help()
								: m.security_top_vulnerabilities_prevalence_help()
						)}
					</h3>
					{@render SegmentToggle(
						m.security_top_vulnerabilities(),
						[
							{ value: 'risk', label: m.security_rank_risk() },
							{ value: 'prevalence', label: m.security_rank_prevalence() }
						],
						findingRank,
						(value) => (findingRank = value === 'prevalence' ? 'prevalence' : 'risk')
					)}
				</div>
				{#if topFindings.length === 0}
					<p class="py-6 text-sm text-muted-foreground">{m.vuln_no_vulnerabilities()}</p>
				{:else}
					<ol class="divide-y divide-border/40">
						{#each topFindings as finding, index (finding.vulnerabilityId)}
							<li>
								<button
									type="button"
									class="-mx-2 grid w-[calc(100%+1rem)] grid-cols-[1.25rem_minmax(0,1fr)_auto] items-center gap-3 rounded-md px-2 py-3 text-left transition-colors hover:bg-muted/40"
									onclick={() => onSelectVulnerability(finding.vulnerabilityId)}
								>
									<span class="text-xs text-muted-foreground tabular-nums">{index + 1}</span>
									<span class="min-w-0 space-y-1">
										<span class="flex flex-wrap items-center gap-2">
											<span
												class="size-2 shrink-0 rounded-full {getSeverityBarClass(finding.severity)}"
												title={getSeverityLabel(finding.severity)}
											></span>
											<span class="font-mono text-sm">{finding.vulnerabilityId}</span>
											{#if finding.knownExploited}
												<Badge variant="red" size="xs">{m.vuln_kev_badge()}</Badge>
											{/if}
											{#if finding.ransomware}
												<Badge variant="red" size="xs">{m.vuln_ransomware_badge()}</Badge>
											{/if}
											{#if finding.epss !== undefined && finding.epss >= 0.01}
												<Badge variant="gray" size="xs">{m.vuln_epss_value({ value: Math.round(finding.epss * 100) })}</Badge>
											{/if}
										</span>
										<span class="block truncate font-mono text-xs text-muted-foreground">
											{finding.pkgName} → {finding.fixedVersion || m.vuln_no_fix()}
											<span class="font-sans">
												· {finding.imagesAffected === 1
													? m.security_images_affected_one()
													: m.security_images_affected({ count: finding.imagesAffected })}
											</span>
										</span>
									</span>
									<span class="text-right">
										{#if findingRank === 'risk'}
											<span class="block text-sm font-semibold tabular-nums">{finding.risk.toFixed(1)}</span>
											<span class="block text-3xs text-muted-foreground">{m.vuln_cvss()} {finding.cvss.toFixed(1)}</span>
										{:else}
											<span class="block text-sm font-semibold tabular-nums">{finding.imagesAffected}</span>
											<span class="block text-3xs text-muted-foreground"
												>{m.security_risk_value({ value: finding.risk.toFixed(1) })}</span
											>
										{/if}
									</span>
								</button>
							</li>
						{/each}
					</ol>
				{/if}
			</section>

			<section class="min-w-0">
				<h3 class="mb-3 text-sm font-medium">{m.security_riskiest_images()}</h3>
				<ul class="divide-y divide-border/40">
					{#each overview.riskiestImages as image (image.imageId)}
						{@const band = getRiskBandDisplay(image.riskBand)}
						<li>
							<a
								href="/images/{image.imageId}?tab=vulnerabilities"
								class="-mx-2 grid grid-cols-[minmax(0,1fr)_auto] items-center gap-4 rounded-md px-2 py-3 transition-colors hover:bg-muted/40"
							>
								<span class="min-w-0 space-y-1">
									<span class="block truncate text-sm font-medium">{image.imageName}</span>
									<span class="block truncate text-xs text-muted-foreground tabular-nums">
										{imageUsageLabel(image)} · {m.security_findings_count({
											count: image.findings
										})}{#if image.knownExploited > 0}
											· {m.vuln_known_exploited_count({ count: image.knownExploited })}{/if}
									</span>
								</span>
								<span class="flex items-center gap-3">
									<span class="h-1.5 w-20 overflow-hidden rounded-full bg-muted" title={band.label}>
										<span class="block h-full rounded-full {band.barClass}" style="width: {image.riskScore}%"></span>
									</span>
									<span class="w-8 text-right text-sm font-semibold tabular-nums">{image.riskScore}</span>
								</span>
							</a>
						</li>
					{/each}
				</ul>
			</section>
		</div>
	</div>
{/if}
