<script lang="ts">
	import { resolve } from '$app/paths';

	import * as StatStrip from '#lib/components/stat-strip/index.js';
	import * as Card from '#lib/components/ui/card/index.js';
	import {
		AlertTriangleIcon,
		ArrowDownIcon,
		ArrowUpIcon,
		EnvironmentsIcon,
		ShieldAlertIcon,
		TrashIcon,
		UpdateIcon
	} from '#lib/icons/index.js';
	import { m } from '#lib/paraglide/messages.js';
	import type { DashboardWidgetId } from '#lib/types/auth.js';
	import { bytes } from '#lib/utils/formatting.js';

	let {
		id,
		healthy,
		attention,
		updates,
		risk,
		reclaimable
	}: {
		id: Extract<DashboardWidgetId, 'healthy' | 'attentionCount' | 'updates' | 'risk' | 'reclaimableTotal'>;
		healthy: { online: number; total: number; offlineNames: string[] };
		attention: { count: number; critical: number; loading: boolean };
		updates: { pending: number; checking: boolean };
		risk: { score: number | null; delta?: number; knownExploited: number; loading: boolean };
		reclaimable: { bytes: number | null; environments: number; loading: boolean };
	} = $props();

	const updatesContext = $derived.by(() => {
		if (updates.checking) return m.dashboard_updates_checking();
		return updates.pending === 0 ? m.dashboard_updates_up_to_date() : m.dashboard_updates_available_label();
	});
</script>

<Card.Root variant="transparent" interactive class="h-full min-w-0">
	<dl class="flex h-full w-full items-center justify-center p-5">
		{#if id === 'healthy'}
			<StatStrip.Item label={m.health_status_healthy()} value={`${healthy.online}/${healthy.total}`} icon={EnvironmentsIcon}>
				<span class="line-clamp-1">
					{healthy.offlineNames.length > 0 ? healthy.offlineNames.join(', ') : m.all_online()}
				</span>
			</StatStrip.Item>
		{:else if id === 'attentionCount'}
			<StatStrip.Item label={m.needs_attention()} value={attention.count} icon={AlertTriangleIcon} loading={attention.loading}>
				{m.count_critical({ count: attention.critical })}
			</StatStrip.Item>
		{:else if id === 'updates'}
			<StatStrip.Item
				label={m.updates()}
				value={updates.pending}
				icon={UpdateIcon}
				href={resolve('/(app)/updates')}
				loading={updates.checking && updates.pending === 0}
			>
				{updatesContext}
			</StatStrip.Item>
		{:else if id === 'risk'}
			<StatStrip.Item
				label={m.risk_score()}
				value={risk.score}
				icon={ShieldAlertIcon}
				href={resolve('/(app)/security')}
				loading={risk.loading}
			>
				<span class="inline-flex items-center gap-1.5">
					{#if risk.delta !== undefined && risk.delta !== 0}
						<span class="inline-flex items-center gap-0.5">
							{#if risk.delta > 0}
								<ArrowUpIcon class="size-3.5 text-destructive" aria-hidden="true" />
							{:else}
								<ArrowDownIcon class="size-3.5 text-success" aria-hidden="true" />
							{/if}
							{Math.abs(risk.delta)}
						</span>
						<span>·</span>
					{/if}
					{risk.score === null ? m.vuln_no_scan() : m.vuln_known_exploited_count({ count: risk.knownExploited })}
				</span>
			</StatStrip.Item>
		{:else}
			<StatStrip.Item
				label={m.reclaimable()}
				value={reclaimable.bytes === null ? null : (bytes.format(reclaimable.bytes, { unitSeparator: ' ' }) ?? '-')}
				icon={TrashIcon}
				loading={reclaimable.loading}
			>
				{#if reclaimable.environments > 1}
					{m.across_count_environments({ count: reclaimable.environments })}
				{/if}
			</StatStrip.Item>
		{/if}
	</dl>
</Card.Root>
