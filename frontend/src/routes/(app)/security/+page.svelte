<script lang="ts">
	import FeatureDisabled from '#lib/components/features/feature-disabled.svelte';
	import { featureStore } from '#lib/stores/features.store.svelte.js';
	import { tryCatch } from '#lib/utils/try-catch.js';

	import { ResourcePageLayout, type ActionButton } from '#lib/layouts/index.js';
	import { m } from '#lib/paraglide/messages.js';
	import { vulnerabilityService } from '#lib/services/vulnerability-service.js';
	import { imageService } from '#lib/services/image-service.js';
	import { extractApiErrorMessage, parallelRefresh } from '#lib/utils/api.js';
	import { useEnvironmentRefresh } from '#lib/hooks/use-environment-refresh.svelte.js';
	import type { VulnerabilityRiskOverview, VulnerabilityWithImage } from '#lib/types/environment.js';
	import type { Paginated, SearchPaginationSortRequest } from '#lib/types/shared.js';
	import { onMount, onDestroy, untrack } from 'svelte';
	import SecurityVulnerabilityTable from './security-vulnerability-table.svelte';
	import SecurityPatchTable from './security-patch-table.svelte';
	import SecurityOverview from './security-overview.svelte';
	import type { ImagePatchTargetDto } from '#lib/types/docker.js';
	import { toast } from 'svelte-sonner';
	import { ActivityIcon, InspectIcon, ShieldAlertIcon, ShieldCheckIcon } from '#lib/icons/index.js';
	import { TabBar, type TabItem } from '#lib/components/tab-bar/index.js';
	import * as Tabs from '#lib/components/ui/tabs/index.js';
	import { environmentStore } from '#lib/stores/environment.store.svelte.js';
	import { activityStore } from '#lib/stores/activity.store.svelte.js';
	import { hasPermission } from '#lib/utils/auth.js';
	import {
		getSeveritySegments,
		mapVulnerabilityPage,
		mapVulnerabilityRequest,
		withVulnerabilityToggles
	} from '#lib/utils/vulnerability.js';
	import { useUrlTab } from '#lib/hooks/use-url-tab.svelte.js';

	let { data } = $props();
	let displayedEnvId = $derived(data.envId);

	let overview = $derived<VulnerabilityRiskOverview | null>(data.overview);
	type VulnerabilityRow = VulnerabilityWithImage & { id: string };

	let vulnerabilities = $derived<Paginated<VulnerabilityRow>>(data.vulnerabilities);
	let requestOptions = $derived<SearchPaginationSortRequest>(data.vulnerabilityRequestOptions);
	let selectedVulnerabilityIds = $state<string[]>([]);
	let showIgnored = $state(false);
	let fixAvailable = $state(false);

	function toggleIgnored(next: boolean) {
		showIgnored = next;
	}

	function toggleFixAvailable(next: boolean) {
		fixAvailable = next;
	}
	let isLoading = $state({ refreshing: false, scanningAll: false });
	let scanProgress = $state({ current: 0, total: 0 });
	const urlTab = useUrlTab({
		validTabs: () => ['overview', 'vulnerabilities', 'patches'],
		defaultTab: () => 'overview',
		// Legacy /images/vulnerabilities?tab=ignored links open the vulnerability table.
		aliases: () => ({ ignored: 'vulnerabilities' })
	});
	const securityTabItems: TabItem[] = [
		{ value: 'overview', label: m.common_overview(), icon: ActivityIcon },
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
		if (!vulnerabilityManagementEnabled) return;
		const requestedEnvId = currentEnvId;
		const operationResult = await tryCatch(
			(async () => {
				const response = await imageService.listPatchTargets(patchRequestOptions);
				if (destroyed || !vulnerabilityManagementEnabled || requestedEnvId !== currentEnvId) return;
				patchTargets = { ...response, data: (response.data ?? []).map((t) => ({ ...t, id: t.imageId })) };
			})()
		);
		if (operationResult.error !== null) {
			const error = operationResult.error;

			console.error('Failed to load image patch targets:', error);
			toast.error(m.common_refresh_failed({ resource: m.patches() }));
		}
	}

	const severityItems = $derived(getSeveritySegments(overview?.summary));

	async function refreshOverview() {
		const requestedEnvId = currentEnvId;
		const result = await tryCatch(vulnerabilityService.getRiskOverviewForEnvironment(requestedEnvId));
		if (result.error !== null) {
			toast.error(m.common_refresh_failed({ resource: m.security() }), { description: extractApiErrorMessage(result.error) });
			return;
		}
		if (!destroyed && vulnerabilityManagementEnabled && requestedEnvId === currentEnvId) overview = result.data;
	}

	async function showVulnerability(vulnerabilityId: string) {
		showIgnored = false;
		fixAvailable = false;
		requestOptions = {
			...requestOptions,
			search: vulnerabilityId,
			pagination: { ...(requestOptions.pagination ?? { limit: 20 }), page: 1 }
		};
		urlTab.select('vulnerabilities');
		await refreshAll();
	}

	// reset drops retained data first so a failed reload leaves the page empty rather than stale.
	async function refreshAll(reset = false) {
		const requestedEnvId = currentEnvId;
		selectedVulnerabilityIds = [];
		if (reset || displayedEnvId !== requestedEnvId) {
			overview = null;
			vulnerabilities = { data: [], pagination: { totalPages: 0, totalItems: 0, currentPage: 1, itemsPerPage: 20 } };
			patchTargets = { data: [], pagination: { totalPages: 0, totalItems: 0, currentPage: 1, itemsPerPage: 20 } };
			displayedEnvId = requestedEnvId;
		}
		await featureStore.refresh(requestedEnvId);
		if (requestedEnvId !== currentEnvId || !vulnerabilityManagementEnabled) return;
		const requestForApi = mapVulnerabilityRequest(withVulnerabilityToggles(requestOptions, { showIgnored, fixAvailable }));
		await parallelRefresh(
			{
				overview: {
					fetch: () => vulnerabilityService.getRiskOverviewForEnvironment(requestedEnvId),
					onSuccess: (data) => {
						if (!destroyed && vulnerabilityManagementEnabled && requestedEnvId === currentEnvId) overview = data;
					},
					errorMessage: m.common_refresh_failed({ resource: m.security() })
				},
				vulnerabilities: {
					fetch: () => vulnerabilityService.getAllVulnerabilitiesForEnvironment(requestedEnvId, requestForApi),
					onSuccess: (data) => {
						if (!destroyed && vulnerabilityManagementEnabled && requestedEnvId === currentEnvId)
							vulnerabilities = mapVulnerabilityPage(data, requestOptions);
					},
					errorMessage: m.common_refresh_failed({ resource: m.vuln_title() })
				},
				patches: {
					fetch: () => imageService.listPatchTargets(patchRequestOptions),
					onSuccess: (data) => {
						if (!destroyed && vulnerabilityManagementEnabled && requestedEnvId === currentEnvId)
							patchTargets = { ...data, data: (data.data ?? []).map((t: ImagePatchTargetDto) => ({ ...t, id: t.imageId })) };
					},
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
		let lastScanned = overview?.drivers.imagesScanned ?? 0;

		stopScanPolling();

		const tick = async () => {
			if (destroyed || !vulnerabilityManagementEnabled) return;
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
			if (destroyed || !vulnerabilityManagementEnabled) return;

			const currentScanned = overview?.drivers.imagesScanned ?? 0;
			const currentTotal = overview?.drivers.imagesTotal ?? targetTotal;

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
		// Ignores made in the table change the overview.
		if (value === 'overview') void refreshOverview();
	}

	onMount(() => {
		void loadPatches();
	});

	useEnvironmentRefresh(refreshAll);

	onMount(() => {
		let activePatchActivityIds = new Set<string>();
		let observedEnvironmentId: string | undefined;
		return activityStore.subscribeActivities((activities) => {
			const environmentId = environmentStore.selected?.id;
			const active = new Set(
				activities
					.filter(
						(activity) =>
							(activity.sourceEnvironmentId || activity.environmentId || '0') === environmentId &&
							(activity.type === 'image_patch' || activity.type === 'vulnerability_scan') &&
							(activity.status === 'queued' || activity.status === 'running')
					)
					.map((activity) => activity.id)
			);
			const finished = observedEnvironmentId === environmentId && [...activePatchActivityIds].some((id) => !active.has(id));
			observedEnvironmentId = environmentId;
			activePatchActivityIds = active;
			if (finished && environmentId && hasPermission('images:read', environmentId)) void loadPatches();
		});
	});

	onDestroy(() => {
		destroyed = true;
		stopScanPolling();
	});

	async function scanAllImages() {
		if (!vulnerabilityManagementEnabled || isLoading.scanningAll) return;

		const requestedEnvId = currentEnvId;
		isLoading.scanningAll = true;
		scanProgress = { current: 0, total: 0 };

		try {
			const operationResult = await tryCatch(
				(async () => {
					// Fetch all images with a high limit to get all of them
					const imagesResponse = await imageService.getImages({
						pagination: { page: 1, limit: 1000 }
					});
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
						if (destroyed || !vulnerabilityManagementEnabled || requestedEnvId !== currentEnvId) return;
						const batch = images.slice(i, i + BATCH_SIZE);

						await Promise.all(
							batch.map(async (image) => {
								const operationResult = await tryCatch(
									(async () => {
										const result = await vulnerabilityService.scanImage(image.id, requestedEnvId);
										if (result.status === 'completed' || result.status === 'scanning' || result.status === 'pending') {
											succeeded++;
										} else {
											failed++;
										}
									})()
								);
								if (operationResult.error !== null) {
									const error = operationResult.error;

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
				})()
			);
			if (operationResult.error !== null) {
				const error = operationResult.error;
				console.error('Error during scan all:', error);
				toast.error(m.security_scan_all_error());
			}
		} finally {
			isLoading.scanningAll = false;
			scanProgress = { current: 0, total: 0 };
		}
	}

	const currentEnvId = $derived(environmentStore.selected?.id || '0');
	const vulnerabilityManagementEnabled = $derived(featureStore.isEnabled('vulnerabilityManagement', currentEnvId));
	let wasFeatureEnabled = true;
	$effect(() => {
		const enabled = vulnerabilityManagementEnabled;
		// Disabled content is hidden by the template; re-enabling reloads it.
		if (!enabled) {
			stopScanPolling();
		} else if (!wasFeatureEnabled) {
			untrack(() => void refreshAll(true));
		}
		wasFeatureEnabled = enabled;
	});
	const canScanVuln = $derived(vulnerabilityManagementEnabled && hasPermission('vulnerabilities:scan', currentEnvId));

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
			onclick: () => refreshAll(),
			loading: isLoading.refreshing,
			disabled: isLoading.refreshing || isLoading.scanningAll
		});
		return buttons;
	});
</script>

<ResourcePageLayout
	title={m.security()}
	subtitle={m.security_subtitle()}
	actionButtons={vulnerabilityManagementEnabled ? actionButtons : []}
>
	{#snippet mainContent()}
		{#if !vulnerabilityManagementEnabled}
			<FeatureDisabled />
		{:else}
			<Tabs.Root value={activeTab}>
				<TabBar items={securityTabItems} value={activeTab} onValueChange={handleTabChange} />
				<Tabs.Content value="overview" class="mt-4">
					{#if overview}
						<SecurityOverview {overview} onSelectVulnerability={showVulnerability} />
					{/if}
				</Tabs.Content>
				<Tabs.Content value="vulnerabilities" class="mt-4">
					{#if overview}
						<div class="mb-3 flex flex-wrap items-center gap-x-5 gap-y-1.5 text-xs text-muted-foreground">
							<span>
								{m.security_images_scanned()}:
								<span class="font-medium text-foreground tabular-nums"
									>{overview.drivers.imagesScanned}/{overview.drivers.imagesTotal}</span
								>
							</span>
							{#each severityItems as item (item.key)}
								<span class="flex items-center gap-1.5">
									<span class="{item.barClass} size-1.5 shrink-0 rounded-full" aria-hidden="true"></span>
									<span class="font-semibold text-foreground tabular-nums">{item.count}</span>
									{item.label}
								</span>
							{/each}
						</div>
					{/if}
					<div class="rounded-xl border border-border/60">
						<SecurityVulnerabilityTable
							bind:vulnerabilities
							bind:requestOptions
							bind:selectedIds={selectedVulnerabilityIds}
							{showIgnored}
							onToggleIgnored={toggleIgnored}
							{fixAvailable}
							onToggleFixAvailable={toggleFixAvailable}
						/>
					</div>
				</Tabs.Content>
				<Tabs.Content value="patches" class="mt-4">
					<div class="rounded-xl border border-border/60">
						<SecurityPatchTable bind:targets={patchTargets} bind:requestOptions={patchRequestOptions} />
					</div>
				</Tabs.Content>
			</Tabs.Root>
		{/if}
	{/snippet}
</ResourcePageLayout>
