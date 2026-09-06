<script lang="ts">
	import { ResourcePageLayout, type ActionButton } from '#lib/layouts/index.js';
	import { m } from '#lib/paraglide/messages.js';
	import { vulnerabilityService } from '#lib/services/vulnerability-service.js';
	import { imageService } from '#lib/services/image-service.js';
	import { parallelRefresh } from '#lib/utils/api.js';
	import { useEnvironmentRefresh } from '#lib/hooks/use-environment-refresh.svelte.js';
	import type { EnvironmentVulnerabilitySummary, VulnerabilityWithImage } from '#lib/types/environment.js';
	import type { Paginated, SearchPaginationSortRequest } from '#lib/types/shared.js';
	import { onMount } from 'svelte';
	import SecurityVulnerabilityTable from './security-vulnerability-table.svelte';
	import SecurityPatchTable from './security-patch-table.svelte';
	import type { ImagePatchTargetDto } from '#lib/types/docker.js';
	import { toast } from 'svelte-sonner';
	import { InspectIcon, ShieldAlertIcon, ShieldCheckIcon } from '#lib/icons/index.js';
	import { TabBar, type TabItem } from '#lib/components/tab-bar/index.js';
	import * as Tabs from '#lib/components/ui/tabs/index.js';
	import { environmentStore } from '#lib/stores/environment.store.svelte.js';
	import { activityStore } from '#lib/stores/activity.store.svelte.js';
	import { hasPermission } from '#lib/utils/auth.js';
	import { mapVulnerabilityPage, mapVulnerabilityRequest } from '#lib/utils/vulnerability.js';
	import { useUrlTab } from '#lib/hooks/use-url-tab.svelte.js';

	let { data } = $props();

	let summary = $derived<EnvironmentVulnerabilitySummary>(data.summary);
	type VulnerabilityRow = VulnerabilityWithImage & { id: string };

	let vulnerabilities = $derived<Paginated<VulnerabilityRow>>(data.vulnerabilities);
	let requestOptions = $derived<SearchPaginationSortRequest>(data.vulnerabilityRequestOptions);
	let showIgnored = $state(false);

	function withIgnoredFilter(options: SearchPaginationSortRequest, show: boolean): SearchPaginationSortRequest {
		const filters = { ...options.filters };
		if (show) {
			filters['ignored'] = 'true';
		} else {
			delete filters['ignored'];
		}

		return {
			...options,
			filters: Object.keys(filters).length > 0 ? filters : undefined
		};
	}

	function toggleIgnored(next: boolean) {
		showIgnored = next;
	}
	let isLoading = $state({ refreshing: false, scanningAll: false });
	let scanProgress = $state({ current: 0, total: 0 });
	const urlTab = useUrlTab({
		validTabs: () => ['vulnerabilities', 'patches'],
		defaultTab: () => 'vulnerabilities'
	});
	const securityTabItems: TabItem[] = [
		{ value: 'vulnerabilities', label: m.vuln_title(), icon: ShieldAlertIcon },
		{ value: 'patches', label: m.patches(), icon: ShieldCheckIcon }
	];
	const activeTab = $derived(urlTab.value);
	let scanPollTimeout: ReturnType<typeof setTimeout> | null = null;
	// Set once on destroy so an in-flight poll tick can't re-arm a timer on a dead component.
	let destroyed = false;

	// Patch targets, loaded when the tab is first opened
	type PatchTargetRow = ImagePatchTargetDto & { id: string };
	let patchTargets = $state<Paginated<PatchTargetRow>>({
		data: [],
		pagination: { totalPages: 0, totalItems: 0, currentPage: 1, itemsPerPage: 20 }
	});
	let patchRequestOptions = $derived<SearchPaginationSortRequest>(data.patchRequestOptions);
	async function loadPatches() {
		try {
			const response = await imageService.listPatchTargets(patchRequestOptions);
			patchTargets = { ...response, data: (response.data ?? []).map((t) => ({ ...t, id: t.imageId })) };
		} catch (error) {
			console.error('Failed to load image patch targets:', error);
			toast.error(m.common_refresh_failed({ resource: m.patches() }));
		}
	}

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
			{ key: 'unknown', value: summaryCounts.unknown, label: m.common_unknown(), dotClass: 'bg-slate-400' }
		];
		return items.filter((item) => item.value > 0);
	});

	async function refreshAll() {
		const requestForApi = mapVulnerabilityRequest(withIgnoredFilter(requestOptions, showIgnored));
		await parallelRefresh(
			{
				summary: {
					fetch: () => vulnerabilityService.getEnvironmentSummary(),
					onSuccess: (data) => (summary = data),
					errorMessage: m.common_refresh_failed({ resource: m.security() })
				},
				vulnerabilities: {
					fetch: () => vulnerabilityService.getAllVulnerabilities(requestForApi),
					onSuccess: (data) => (vulnerabilities = mapVulnerabilityPage(data, requestOptions)),
					errorMessage: m.common_refresh_failed({ resource: m.vuln_title() })
				},
				patches: {
					fetch: () => imageService.listPatchTargets(patchRequestOptions),
					onSuccess: (data) =>
						(patchTargets = { ...data, data: (data.data ?? []).map((t: ImagePatchTargetDto) => ({ ...t, id: t.imageId })) }),
					errorMessage: m.common_refresh_failed({ resource: m.patches() })
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
			if (destroyed) return;
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
			if (destroyed) return;

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

	function handleTabChange(value: string) {
		urlTab.select(value);
	}

	onMount(() => {
		void loadPatches();
	});

	useEnvironmentRefresh(refreshAll);

	let activePatchActivityIds = new Set<string>();
	$effect(() => {
		const active = new Set(
			activityStore.activities
				.filter(
					(a) =>
						(a.type === 'image_patch' || a.type === 'vulnerability_scan') && (a.status === 'queued' || a.status === 'running')
				)
				.map((a) => a.id)
		);
		const finished = [...activePatchActivityIds].some((id) => !active.has(id));
		activePatchActivityIds = active;
		if (finished) void loadPatches();
	});

	$effect(() => () => {
		destroyed = true;
		stopScanPolling();
	});

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

	const currentEnvId = $derived(environmentStore.selected?.id || '0');
	const canScanVuln = $derived(hasPermission('vulnerabilities:scan', currentEnvId));

	const actionButtons: ActionButton[] = $derived.by(() => {
		const buttons: ActionButton[] = [];
		if (canScanVuln) {
			buttons.push({
				id: 'scan-all',
				action: 'base',
				label: isLoading.scanningAll ? `${m.scanning()} (${scanProgress.current}/${scanProgress.total})` : m.security_scan_all(),
				onclick: scanAllImages,
				loading: isLoading.scanningAll,
				disabled: isLoading.scanningAll || isLoading.refreshing,
				icon: InspectIcon
			});
		}
		buttons.push({
			id: 'refresh',
			action: 'restart',
			label: m.common_refresh(),
			onclick: refreshAll,
			loading: isLoading.refreshing,
			disabled: isLoading.refreshing || isLoading.scanningAll
		});
		return buttons;
	});
</script>

<ResourcePageLayout title={m.security()} subtitle={m.security_subtitle()} {actionButtons}>
	{#snippet mainContent()}
		<div class="space-y-6">
			<div class="rounded-lg border border-border/40 bg-muted/20 px-4 py-3">
				<div class="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
					<div class="flex items-baseline gap-4 text-xs text-muted-foreground">
						<span
							>{m.security_images_scanned()}:
							<span class="font-medium text-foreground tabular-nums">{imagesScannedLabel}</span></span
						>
						<span
							>{m.security_total_vulnerabilities()}:
							<span class="font-medium text-foreground tabular-nums">{summaryCounts.total}</span></span
						>
					</div>
					{#if severityItems.length > 0}
						<div class="flex flex-wrap items-center gap-x-4 gap-y-1.5">
							{#each severityItems as item (item.key)}
								<div class="flex items-center gap-1.5">
									<span class="{item.dotClass} h-1.5 w-1.5 shrink-0 rounded-full" aria-hidden="true"></span>
									<span class="text-xs text-muted-foreground">
										<span class="font-semibold text-foreground tabular-nums">{item.value}</span>
										<span class="ml-0.5">{item.label}</span>
									</span>
								</div>
							{/each}
						</div>
					{/if}
				</div>
			</div>

			<Tabs.Root value={activeTab}>
				<TabBar items={securityTabItems} value={activeTab} onValueChange={handleTabChange} />
				<Tabs.Content value="vulnerabilities" class="mt-4">
					<div class="rounded-xl border border-border/60">
						<SecurityVulnerabilityTable bind:vulnerabilities bind:requestOptions {showIgnored} onToggleIgnored={toggleIgnored} />
					</div>
				</Tabs.Content>
				<Tabs.Content value="patches" class="mt-4">
					<div class="rounded-xl border border-border/60">
						<SecurityPatchTable bind:targets={patchTargets} bind:requestOptions={patchRequestOptions} />
					</div>
				</Tabs.Content>
			</Tabs.Root>
		</div>
	{/snippet}
</ResourcePageLayout>
