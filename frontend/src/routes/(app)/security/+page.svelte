<script lang="ts">
	import { ResourcePageLayout, type ActionButton, type StatCardConfig } from '$lib/layouts/index.js';
	import StatCard from '$lib/components/stat-card.svelte';
	import { m } from '$lib/paraglide/messages';
	import { vulnerabilityService } from '$lib/services/vulnerability-service';
	import { parallelRefresh } from '$lib/utils/refresh.util';
	import { useEnvironmentRefresh } from '$lib/hooks/use-environment-refresh.svelte';
	import type { EnvironmentVulnerabilitySummary, VulnerabilityWithImage } from '$lib/types/vulnerability.type';
	import type { Paginated, SearchPaginationSortRequest } from '$lib/types/pagination.type';
	import { ShieldAlertIcon, ShieldCheckIcon, AlertTriangleIcon, ShieldXIcon, ImagesIcon } from '$lib/icons';
	import { untrack } from 'svelte';
	import SecurityVulnerabilityTable from './security-vulnerability-table.svelte';

	let { data } = $props();

	let summary = $state<EnvironmentVulnerabilitySummary>(untrack(() => data.summary));
	type VulnerabilityRow = VulnerabilityWithImage & { id: string };

	let vulnerabilities = $state<Paginated<VulnerabilityRow>>(untrack(() => data.vulnerabilities));
	let requestOptions = $state<SearchPaginationSortRequest>(untrack(() => data.vulnerabilityRequestOptions));
	let isLoading = $state({ refreshing: false });

	const summaryCounts = $derived.by(() => ({
		critical: summary?.summary?.critical ?? 0,
		high: summary?.summary?.high ?? 0,
		medium: summary?.summary?.medium ?? 0,
		low: summary?.summary?.low ?? 0,
		unknown: summary?.summary?.unknown ?? 0,
		total: summary?.summary?.total ?? 0
	}));

	const imagesScannedLabel = $derived.by(() => {
		const total = summary?.totalImages ?? 0;
		const scanned = summary?.scannedImages ?? 0;
		return `${scanned}/${total}`;
	});

	function mapVulnerabilityRequest(options: SearchPaginationSortRequest): SearchPaginationSortRequest {
		const filters = { ...(options.filters ?? {}) };
		if (filters.vulnSeverity) {
			filters.severity = filters.vulnSeverity;
			delete filters.vulnSeverity;
		}

		const sort = options.sort?.column === 'vulnSeverity' ? { ...options.sort, column: 'severity' } : options.sort;

		return {
			...options,
			sort,
			filters: Object.keys(filters).length ? filters : undefined
		};
	}

	function getVulnerabilityKey(vuln: VulnerabilityWithImage, index: number): string {
		return [
			vuln.imageId,
			vuln.vulnerabilityId,
			vuln.pkgName,
			vuln.installedVersion ?? '',
			vuln.fixedVersion ?? '',
			String(index)
		].join('-');
	}

	function mapVulnerabilityPage(page: Paginated<VulnerabilityWithImage>, options: SearchPaginationSortRequest) {
		const pageNumber = options.pagination?.page ?? page.pagination?.currentPage ?? 1;
		const limit = options.pagination?.limit ?? page.pagination?.itemsPerPage ?? 20;
		const offset = (pageNumber - 1) * limit;
		return {
			...page,
			data: (page.data ?? []).map((item, index) => ({
				...item,
				id: getVulnerabilityKey(item, offset + index)
			}))
		};
	}

	async function refreshAll() {
		const requestForApi = mapVulnerabilityRequest(requestOptions);
		await parallelRefresh(
			{
				summary: {
					fetch: () => vulnerabilityService.getEnvironmentSummary(),
					onSuccess: (data) => (summary = data),
					errorMessage: m.common_refresh_failed({ resource: m.security_title() })
				},
				vulnerabilities: {
					fetch: () => vulnerabilityService.getAllVulnerabilities(requestForApi),
					onSuccess: (data) => (vulnerabilities = mapVulnerabilityPage(data, requestOptions)),
					errorMessage: m.common_refresh_failed({ resource: m.vuln_title() })
				}
			},
			(v) => (isLoading.refreshing = v)
		);
	}

	useEnvironmentRefresh(refreshAll);

	const actionButtons: ActionButton[] = $derived([
		{
			id: 'refresh',
			action: 'restart',
			label: m.common_refresh(),
			onclick: refreshAll,
			loading: isLoading.refreshing,
			disabled: isLoading.refreshing
		}
	]);

	const statCards: StatCardConfig[] = $derived([
		{
			title: m.security_images_scanned(),
			value: imagesScannedLabel,
			icon: ImagesIcon,
			iconColor: 'text-blue-500'
		},
		{
			title: m.security_total_vulnerabilities(),
			value: summaryCounts.total,
			icon: ShieldAlertIcon,
			iconColor: 'text-red-500'
		}
	]);

	const severityCards = $derived.by(() =>
		[
			{
				key: 'critical',
				title: m.vuln_severity_critical(),
				value: summaryCounts.critical,
				icon: AlertTriangleIcon,
				iconColor: 'text-red-500'
			},
			{
				key: 'high',
				title: m.vuln_severity_high(),
				value: summaryCounts.high,
				icon: ShieldAlertIcon,
				iconColor: 'text-orange-500'
			},
			{
				key: 'medium',
				title: m.vuln_severity_medium(),
				value: summaryCounts.medium,
				icon: ShieldAlertIcon,
				iconColor: 'text-amber-500'
			},
			{
				key: 'low',
				title: m.vuln_severity_low(),
				value: summaryCounts.low,
				icon: ShieldCheckIcon,
				iconColor: 'text-emerald-500'
			},
			{
				key: 'unknown',
				title: m.vuln_severity_unknown(),
				value: summaryCounts.unknown,
				icon: ShieldXIcon,
				iconColor: 'text-slate-400'
			}
		].filter((card) => card.value > 0)
	);
</script>

<ResourcePageLayout title={m.security_title()} subtitle={m.security_subtitle()} {actionButtons} {statCards}>
	{#snippet mainContent()}
		<div class="space-y-6">
			{#if severityCards.length > 0}
				<div class="border-border/50 bg-muted/30 rounded-xl border">
					<div class="flex flex-wrap items-center gap-x-4 gap-y-2 px-4 py-2">
						{#each severityCards as card (card.key)}
							<StatCard variant="mini" title={card.title} value={card.value} icon={card.icon} iconColor={card.iconColor} />
						{/each}
					</div>
				</div>
			{/if}

			<div class="border-border/60 rounded-xl border">
				<SecurityVulnerabilityTable bind:vulnerabilities bind:requestOptions />
			</div>
		</div>
	{/snippet}
</ResourcePageLayout>
