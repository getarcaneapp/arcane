<script lang="ts">
	import { onMount } from 'svelte';
	import { environmentStore } from '$lib/stores/environment.store.svelte';
	import { createStatsWebSocket } from '$lib/utils/ws';
	import type { ReconnectingWebSocket } from '$lib/utils/ws';
	import type { SystemStats } from '$lib/types/system-stats.type';
	import SchedulerMetrics from '$lib/components/scheduler-metrics.svelte';
	import { InfoIcon, StatsIcon } from '$lib/icons';
	import { SettingsPageLayout, type SettingsActionButton } from '$lib/layouts';
	import * as Card from '$lib/components/ui/card';

	let systemStats = $state<SystemStats | null>(null);
	let isLoading = $state(true);
	let statsWSClient: ReconnectingWebSocket<SystemStats> | null = null;

	function setupStatsWS() {
		if (statsWSClient) {
			statsWSClient.close();
			statsWSClient = null;
		}

		const getEnvId = () => {
			const env = environmentStore.selected;
			return env ? env.id : '0';
		};

		statsWSClient = createStatsWebSocket({
			getEnvId,
			includeRuntimeMetrics: true,
			onOpen: () => {
				isLoading = true;
			},
			onMessage: (data) => {
				systemStats = data;
				isLoading = false;
			},
			onError: (e) => {
				console.error('Stats websocket error:', e);
			}
		});
		statsWSClient.connect();
	}

	onMount(() => {
		setupStatsWS();

		return () => {
			statsWSClient?.close();
		};
	});

	$effect(() => {
		const env = environmentStore.selected;
		if (env) {
			setupStatsWS();
		}
	});

	const currentStats = $derived(systemStats);
	const actionButtons = $derived.by((): SettingsActionButton[] => [
		{
			id: 'live',
			action: 'base',
			label: isLoading ? '● Connecting…' : '● Live',
			onclick: () => {},
			disabled: true
		},
		{
			id: 'refresh',
			action: 'base',
			label: 'Refresh',
			onclick: () => setupStatsWS()
		}
	]);
</script>

<SettingsPageLayout
	title="System Details"
	description="Detailed Go runtime and scheduler metrics for the Arcane backend."
	icon={StatsIcon}
	pageType="management"
	{actionButtons}
>
	{#snippet mainContent()}
		<div class="space-y-6">
			<Card.Root class="border-border/70 bg-surface/40 shadow-[0_10px_30px_rgba(0,0,0,0.35)]">
				<Card.Header class="border-border/60 bg-muted/20 border-b pb-3">
					<div class="flex items-center justify-between">
						<Card.Title class="text-sm font-medium">Environment</Card.Title>
						<InfoIcon class="text-muted-foreground size-4" />
					</div>
				</Card.Header>
				<Card.Content class="grid gap-3 py-4 sm:grid-cols-3">
					<div class="border-border/60 bg-muted/20 rounded-lg border px-3 py-2">
						<span class="text-muted-foreground block text-[10px] font-semibold tracking-wider uppercase">Platform</span>
						<p class="font-mono text-sm">{currentStats?.platform ?? '-'} / {currentStats?.architecture ?? '-'}</p>
					</div>
					<div class="border-border/60 bg-muted/20 rounded-lg border px-3 py-2">
						<span class="text-muted-foreground block text-[10px] font-semibold tracking-wider uppercase">Hostname</span>
						<p class="font-mono text-sm">{currentStats?.hostname ?? '-'}</p>
					</div>
					<div class="border-border/60 bg-muted/20 rounded-lg border px-3 py-2">
						<span class="text-muted-foreground block text-[10px] font-semibold tracking-wider uppercase">CPU Cores</span>
						<p class="font-mono text-sm">{currentStats?.cpuCount ?? '-'}</p>
					</div>
				</Card.Content>
			</Card.Root>

			<div class="grid grid-cols-1 gap-6 xl:grid-cols-[minmax(0,1.2fr)_minmax(0,1fr)]">
				<SchedulerMetrics
					stats={currentStats?.goroutines}
					threads={currentStats?.threads}
					runtime={currentStats?.runtime}
					loading={isLoading && !currentStats}
				/>

				<Card.Root class="border-border/70 bg-surface/40 shadow-[0_10px_30px_rgba(0,0,0,0.35)]">
					<Card.Header class="border-border/60 bg-muted/20 border-b pb-4">
						<div class="flex items-center justify-between">
							<Card.Title class="text-sm font-medium">Runtime Metrics</Card.Title>
							<StatsIcon class="text-muted-foreground size-4" />
						</div>
						<Card.Description class="text-xs">Full runtime/metrics snapshot</Card.Description>
					</Card.Header>
					<Card.Content class="py-4">
						{#if isLoading && !currentStats}
							<div class="bg-muted h-40 animate-pulse rounded"></div>
						{:else}
							<div class="border-border/60 bg-muted/10 max-h-[520px] overflow-hidden rounded-lg border">
								<div class="max-h-[520px] overflow-auto">
									<div
										class="bg-background/70 text-muted-foreground sticky top-0 z-10 grid min-w-0 grid-cols-[minmax(0,1fr)_auto_auto] gap-x-4 px-4 py-2 text-[10px] font-semibold tracking-wider uppercase backdrop-blur"
									>
										<div class="min-w-0">Metric</div>
										<div class="text-right">Value</div>
										<div>Unit</div>
									</div>
									<div class="divide-border/40 divide-y">
										{#each currentStats?.runtimeMetrics ?? [] as metric}
											<div
												class="hover:bg-muted/40 grid min-w-0 grid-cols-[minmax(0,1fr)_auto_auto] items-center gap-x-4 px-4 py-2 text-xs"
											>
												<div
													class="text-foreground/90 min-w-0 truncate font-mono"
													title={`${metric.name}\n${metric.description}`}
												>
													{metric.name}
												</div>
												<div class="text-right font-medium tabular-nums">
													{metric.value || '-'}
												</div>
												<div class="text-muted-foreground">{metric.unit}</div>
											</div>
										{/each}
									</div>
								</div>
							</div>
						{/if}
					</Card.Content>
				</Card.Root>
			</div>
		</div>
	{/snippet}
</SettingsPageLayout>
