<script lang="ts">
	import { ResourcePageLayout, type ActionButton } from '$lib/layouts/index.js';
	import { m } from '$lib/paraglide/messages';
	import { vulnerabilityService } from '$lib/services/vulnerability-service';
	import { parallelRefresh } from '$lib/utils/refresh.util';
	import { useEnvironmentRefresh } from '$lib/hooks/use-environment-refresh.svelte';
	import type { EnvironmentVulnerabilitySummary, VulnerabilityWithImage } from '$lib/types/vulnerability.type';
	import type { Paginated, SearchPaginationSortRequest } from '$lib/types/pagination.type';
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

	const severityItems = $derived.by(() => {
		const items = [
			{ key: 'critical', value: summaryCounts.critical, label: m.vuln_severity_critical(), dotClass: 'bg-red-500' },
			{ key: 'high', value: summaryCounts.high, label: m.vuln_severity_high(), dotClass: 'bg-orange-500' },
			{ key: 'medium', value: summaryCounts.medium, label: m.vuln_severity_medium(), dotClass: 'bg-amber-500' },
			{ key: 'low', value: summaryCounts.low, label: m.vuln_severity_low(), dotClass: 'bg-emerald-500' },
			{ key: 'unknown', value: summaryCounts.unknown, label: m.vuln_severity_unknown(), dotClass: 'bg-slate-400' }
		];
		return items.filter((item) => item.value > 0);
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
</script>

<ResourcePageLayout title={m.security_title()} subtitle={m.security_subtitle()} {actionButtons}>
	{#snippet mainContent()}
		<div class="space-y-6">
			<!-- Minimal overview: one compact block -->
			<div class="border-border/40 bg-muted/20 rounded-lg border px-4 py-3">
				<div class="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
					<div class="text-muted-foreground flex items-baseline gap-4 text-xs">
						<span
							>{m.security_images_scanned()}:
							<span class="text-foreground font-medium tabular-nums">{imagesScannedLabel}</span></span
						>
						<span
							>{m.security_total_vulnerabilities()}:
							<span class="text-foreground font-medium tabular-nums">{summaryCounts.total}</span></span
						>
					</div>
					{#if severityItems.length > 0}
						<div class="flex flex-wrap items-center gap-x-4 gap-y-1.5">
							{#each severityItems as item (item.key)}
								<div class="flex items-center gap-1.5">
									<span class="{item.dotClass} h-1.5 w-1.5 shrink-0 rounded-full" aria-hidden="true"></span>
									<span class="text-muted-foreground text-xs">
										<span class="text-foreground font-semibold tabular-nums">{item.value}</span>
										<span class="ml-0.5">{item.label}</span>
									</span>
								</div>
							{/each}
						</div>
					{/if}
				</div>
			</div>

			<div class="border-border/60 rounded-xl border">
				<SecurityVulnerabilityTable bind:vulnerabilities bind:requestOptions />
			</div>
		</div>
	{/snippet}
</ResourcePageLayout>
