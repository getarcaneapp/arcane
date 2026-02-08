<script lang="ts">
	import { ResourcePageLayout, type ActionButton } from '$lib/layouts/index.js';
	import { m } from '$lib/paraglide/messages';
	import { vulnerabilityService } from '$lib/services/vulnerability-service';
	import { imageService } from '$lib/services/image-service';
	import { parallelRefresh } from '$lib/utils/refresh.util';
	import { useEnvironmentRefresh } from '$lib/hooks/use-environment-refresh.svelte';
	import type {
		EnvironmentVulnerabilitySummary,
		VulnerabilityWithImage,
		IgnoredVulnerability
	} from '$lib/types/vulnerability.type';
	import type { Paginated, SearchPaginationSortRequest } from '$lib/types/pagination.type';
	import { untrack } from 'svelte';
	import SecurityVulnerabilityTable from './security-vulnerability-table.svelte';
	import IgnoredVulnerabilitiesTable from './ignored-vulnerabilities-table.svelte';
	import { toast } from 'svelte-sonner';
	import { InspectIcon } from '$lib/icons';
	import * as Tabs from '$lib/components/ui/tabs/index.js';

	let { data } = $props();

	let summary = $state<EnvironmentVulnerabilitySummary>(untrack(() => data.summary));
	type VulnerabilityRow = VulnerabilityWithImage & { id: string };

	let vulnerabilities = $state<Paginated<VulnerabilityRow>>(untrack(() => data.vulnerabilities));
	let requestOptions = $state<SearchPaginationSortRequest>(untrack(() => data.vulnerabilityRequestOptions));
	let isLoading = $state({ refreshing: false, scanningAll: false });
	let scanProgress = $state({ current: 0, total: 0 });
	let activeTab = $state('vulnerabilities');
	let scanPollTimeout: ReturnType<typeof setTimeout> | null = null;

	// Ignored vulnerabilities state
	let ignoredVulnerabilities = $state<Paginated<IgnoredVulnerability>>({
		data: [],
		pagination: { totalPages: 0, totalItems: 0, currentPage: 1, itemsPerPage: 20 }
	});
	let ignoredRequestOptions = $state<SearchPaginationSortRequest>({
		pagination: { page: 1, limit: 20 }
	});
	let isLoadingIgnored = $state(false);

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

	function stopScanPolling() {
		if (scanPollTimeout) {
			clearTimeout(scanPollTimeout);
			scanPollTimeout = null;
		}
	}

	function startScanPolling(targetTotal: number) {
		const POLL_INTERVAL_MS = 5000;
		const MAX_ATTEMPTS = 24;
		const MAX_IDLE_TICKS = 3;
		let attempts = 0;
		let idleTicks = 0;
		let lastScanned = summary?.scannedImages ?? 0;

		stopScanPolling();

		const tick = async () => {
			if (attempts >= MAX_ATTEMPTS) {
				stopScanPolling();
				return;
			}
			attempts++;

			if (isLoading.refreshing) {
				scanPollTimeout = setTimeout(tick, POLL_INTERVAL_MS);
				return;
			}

			await refreshAll();

			const currentScanned = summary?.scannedImages ?? 0;
			const currentTotal = summary?.totalImages ?? targetTotal;

			if (currentTotal > 0 && currentScanned >= currentTotal) {
				stopScanPolling();
				return;
			}

			if (currentScanned === lastScanned) {
				idleTicks++;
			} else {
				idleTicks = 0;
				lastScanned = currentScanned;
			}

			if (idleTicks >= MAX_IDLE_TICKS) {
				stopScanPolling();
				return;
			}

			scanPollTimeout = setTimeout(tick, POLL_INTERVAL_MS);
		};

		scanPollTimeout = setTimeout(tick, POLL_INTERVAL_MS);
	}

	async function loadIgnoredVulnerabilities(options?: SearchPaginationSortRequest) {
		if (isLoadingIgnored) return;
		isLoadingIgnored = true;
		try {
			const request = options ?? ignoredRequestOptions;
			const response = await vulnerabilityService.getIgnoredVulnerabilities(request);
			ignoredVulnerabilities = response;
			if (options) {
				ignoredRequestOptions = options;
			}
		} catch (error) {
			console.error('Failed to load ignored vulnerabilities:', error);
			toast.error(m.common_refresh_failed({ resource: m.vuln_ignored_title() }));
		} finally {
			isLoadingIgnored = false;
		}
	}

	async function handleUnignore(ignoreId: string) {
		try {
			await vulnerabilityService.unignoreVulnerability(ignoreId);
			toast.success(m.vuln_unignore_success());
			// Refresh both lists
			await loadIgnoredVulnerabilities();
			await refreshAll();
		} catch (error) {
			console.error('Failed to unignore vulnerability:', error);
			toast.error(m.vuln_unignore_failed());
		}
	}

	function handleTabChange(value: string) {
		activeTab = value;
		if (value === 'ignored' && ignoredVulnerabilities.data.length === 0) {
			loadIgnoredVulnerabilities();
		}
	}

	useEnvironmentRefresh(refreshAll);

	$effect(() => () => stopScanPolling());

	async function scanAllImages() {
		if (isLoading.scanningAll) return;

		isLoading.scanningAll = true;
		scanProgress = { current: 0, total: 0 };

		try {
			// Fetch all images with a high limit to get all of them
			const imagesResponse = await imageService.getImages({ pagination: { page: 1, limit: 1000 } });
			const images = imagesResponse.data ?? [];

			if (images.length === 0) {
				toast.info(m.security_no_images_to_scan());
				isLoading.scanningAll = false;
				return;
			}

			scanProgress = { current: 0, total: images.length };

			const BATCH_SIZE = 3;
			let succeeded = 0;
			let failed = 0;

			for (let i = 0; i < images.length; i += BATCH_SIZE) {
				const batch = images.slice(i, i + BATCH_SIZE);

				await Promise.all(
					batch.map(async (image) => {
						try {
							const result = await vulnerabilityService.scanImage(image.id);
							if (result.status === 'completed' || result.status === 'scanning' || result.status === 'pending') {
								succeeded++;
							} else {
								failed++;
							}
						} catch (error) {
							console.error(`Failed to scan image ${image.id}:`, error);
							failed++;
						}
						scanProgress.current++;
					})
				);
			}

			// Show summary toast (scans run in background; this reflects requests started, not completed)
			if (failed === 0) {
				toast.success(m.security_scan_all_success({ count: succeeded }));
			} else if (succeeded === 0) {
				toast.error(m.security_scan_all_failed({ count: failed }));
			} else {
				toast.warning(m.security_scan_all_partial({ succeeded, failed }));
			}

			// Refresh the vulnerability data and keep polling for updates as scans complete
			await refreshAll();
			startScanPolling(images.length);
		} catch (error) {
			console.error('Error during scan all:', error);
			toast.error(m.security_scan_all_error());
		} finally {
			isLoading.scanningAll = false;
			scanProgress = { current: 0, total: 0 };
		}
	}

	const actionButtons: ActionButton[] = $derived([
		{
			id: 'scan-all',
			action: 'base',
			label: isLoading.scanningAll
				? `${m.security_scanning()} (${scanProgress.current}/${scanProgress.total})`
				: m.security_scan_all(),
			onclick: scanAllImages,
			loading: isLoading.scanningAll,
			disabled: isLoading.scanningAll || isLoading.refreshing,
			icon: InspectIcon
		},
		{
			id: 'refresh',
			action: 'restart',
			label: m.common_refresh(),
			onclick: refreshAll,
			loading: isLoading.refreshing,
			disabled: isLoading.refreshing || isLoading.scanningAll
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

			<Tabs.Root value={activeTab} onValueChange={handleTabChange}>
				<Tabs.List class="grid w-full grid-cols-2">
					<Tabs.Trigger value="vulnerabilities">{m.vuln_title()}</Tabs.Trigger>
					<Tabs.Trigger value="ignored">{m.vuln_ignored_title()}</Tabs.Trigger>
				</Tabs.List>
				<Tabs.Content value="vulnerabilities" class="mt-4">
					<div class="border-border/60 rounded-xl border">
						<SecurityVulnerabilityTable bind:vulnerabilities bind:requestOptions />
					</div>
				</Tabs.Content>
				<Tabs.Content value="ignored" class="mt-4">
					<div class="border-border/60 rounded-xl border">
						<IgnoredVulnerabilitiesTable
							{ignoredVulnerabilities}
							requestOptions={ignoredRequestOptions}
							isLoading={isLoadingIgnored}
							onRefresh={loadIgnoredVulnerabilities}
							onUnignore={handleUnignore}
						/>
					</div>
				</Tabs.Content>
			</Tabs.Root>
		</div>
	{/snippet}
</ResourcePageLayout>
