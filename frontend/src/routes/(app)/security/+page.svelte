<script lang="ts">
	import { onMount, onDestroy, untrack } from 'svelte';
	import { toast } from 'svelte-sonner';

	import FeatureDisabled from '#lib/components/features/feature-disabled.svelte';
	import EnvironmentDestinationSelect from '#lib/components/form/environment-destination-select.svelte';
	import { EmptyState } from '#lib/components/states/index.js';
	import { TabBar, type TabItem } from '#lib/components/tab-bar/index.js';
	import * as Tabs from '#lib/components/ui/tabs/index.js';
	import { TABLE_PAGE_SIZE_ALL } from '#lib/constants/table-pagination.js';
	import { useEnvironmentRefresh } from '#lib/hooks/use-environment-refresh.svelte.js';
	import { useUrlTab } from '#lib/hooks/use-url-tab.svelte.js';
	import { ActivityIcon, ImagesIcon, InspectIcon, ScanIcon, ShieldAlertIcon, ShieldCheckIcon } from '#lib/icons/index.js';
	import { ResourcePageLayout, type ActionButton } from '#lib/layouts/index.js';
	import { m } from '#lib/paraglide/messages.js';
	import { imageService } from '#lib/services/image-service.js';
	import { vulnerabilityService } from '#lib/services/vulnerability-service.js';
	import { activityStore } from '#lib/stores/activity.store.svelte.js';
	import { environmentStore } from '#lib/stores/environment.store.svelte.js';
	import { featureStore } from '#lib/stores/features.store.svelte.js';
	import type { VulnerabilityRiskOverview } from '#lib/types/environment.js';
	import type { Paginated, SearchPaginationSortRequest } from '#lib/types/shared.js';
	import { extractApiErrorMessage, parallelRefresh } from '#lib/utils/api.js';
	import { hasPermission } from '#lib/utils/auth.js';
	import { fetchAcrossEnvironments } from '#lib/utils/environments.js';
	import { tryCatch } from '#lib/utils/try-catch.js';
	import {
		getSeveritySegments,
		mapVulnerabilityPage,
		mapVulnerabilityRequest,
		vulnerabilityEnvironments,
		vulnerabilityOverviewEnvironment,
		vulnerabilityOverviewEnvironments,
		withVulnerabilityToggles,
		type VulnerabilityRow
	} from '#lib/utils/vulnerability.js';

	import SecurityOverview from './components/security-overview.svelte';
	import SecurityPatchTable, { fetchPatchTargets, type PatchTargetPage } from './components/security-patch-table.svelte';
	import SecurityVulnerabilityTable from './components/security-vulnerability-table.svelte';

	let { data } = $props();
	let displayedEnvId = $derived(data.envId);
	// Rows carry their environment, so they are cleared when the shown environments change too.
	let displayedShownKey = environmentStore.shownKey;

	let overview = $derived<VulnerabilityRiskOverview | null>(data.overview);

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
	let refreshGeneration = 0;

	// Patch targets, loaded when the tab is first opened
	let patchTargets = $state<PatchTargetPage>({
		data: [],
		pagination: { totalPages: 0, totalItems: 0, currentPage: 1, itemsPerPage: 20 }
	});
	let patchRequestOptions = $derived<SearchPaginationSortRequest>(data.patchRequestOptions);
	async function loadPatches() {
		if (!vulnerabilityManagementEnabled) return;
		const requestedScope = scopeKey();
		const operationResult = await tryCatch(
			(async () => {
				const response = await fetchPatchTargets(patchRequestOptions);
				if (destroyed || !vulnerabilityManagementEnabled || requestedScope !== scopeKey()) return;
				patchTargets = response;
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
		const requestedScope = scopeKey();
		const result = await tryCatch(vulnerabilityService.getRiskOverviewForEnvironment(overviewEnvironmentId));
		if (result.error !== null) {
			toast.error(m.common_refresh_failed({ resource: m.security() }), { description: extractApiErrorMessage(result.error) });
			return;
		}
		if (!destroyed && vulnerabilityManagementEnabled && requestedScope === scopeKey()) overview = result.data;
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
		const requestedScope = scopeKey();
		// A newer refresh wins, so an older one finishing last cannot put removed environments back.
		const generation = ++refreshGeneration;
		const stillCurrent = () =>
			!destroyed && vulnerabilityManagementEnabled && requestedScope === scopeKey() && generation === refreshGeneration;
		selectedVulnerabilityIds = [];
		if (reset || displayedEnvId !== requestedEnvId || displayedShownKey !== environmentStore.shownKey) {
			overview = null;
			vulnerabilities = { data: [], pagination: { totalPages: 0, totalItems: 0, currentPage: 1, itemsPerPage: 20 } };
			patchTargets = { data: [], pagination: { totalPages: 0, totalItems: 0, currentPage: 1, itemsPerPage: 20 } };
			displayedEnvId = requestedEnvId;
			displayedShownKey = environmentStore.shownKey;
		}
		await Promise.all(environmentStore.shown.map((env) => featureStore.refresh(env.id)));
		if (!stillCurrent()) return;
		const requestForApi = mapVulnerabilityRequest(withVulnerabilityToggles(requestOptions, { showIgnored, fixAvailable }));
		await parallelRefresh(
			{
				overview: {
					fetch: () => vulnerabilityService.getRiskOverviewForEnvironment(overviewEnvironmentId),
					onSuccess: (data) => {
						if (stillCurrent()) overview = data;
					},
					errorMessage: m.common_refresh_failed({ resource: m.security() })
				},
				vulnerabilities: {
					fetch: () =>
						fetchAcrossEnvironments(vulnerabilityEnvironments(), requestForApi, (id, request, config) =>
							vulnerabilityService.getAllVulnerabilitiesForEnvironment(id, request, config)
						),
					onSuccess: (data) => {
						if (stillCurrent()) vulnerabilities = mapVulnerabilityPage(data, requestOptions);
					},
					errorMessage: m.common_refresh_failed({ resource: m.vuln_title() })
				},
				patches: {
					fetch: () => fetchPatchTargets(patchRequestOptions),
					onSuccess: (data) => {
						if (stillCurrent()) patchTargets = data;
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
		// Activity ID to type for the patches and scans still running.
		let activeActivities = new Map<string, string>();
		let observedScope: string | undefined;
		return activityStore.subscribeActivities((activities) => {
			// Patches and scans run in each row's own environment, so every shown one is watched.
			const environmentIds = environmentStore.shown.map((env) => env.id);
			const active = new Map(
				activities
					.filter(
						(activity) =>
							environmentIds.includes(activity.sourceEnvironmentId || activity.environmentId || '0') &&
							(activity.type === 'image_patch' || activity.type === 'vulnerability_scan') &&
							(activity.status === 'queued' || activity.status === 'running')
					)
					.map((activity) => [activity.id, activity.type] as const)
			);
			const scope = environmentStore.shownKey;
			const finished = observedScope === scope ? [...activeActivities].filter(([id]) => !active.has(id)) : [];
			observedScope = scope;
			activeActivities = active;
			// Scan polling follows one environment's overview, so with several shown a finished scan refreshes findings.
			if (environmentIds.length > 1 && finished.some(([, type]) => type === 'vulnerability_scan')) void refreshAll();
			else if (finished.length > 0 && environmentIds.some((id) => hasPermission('images:read', id))) void loadPatches();
		});
	});

	onDestroy(() => {
		destroyed = true;
		stopScanPolling();
	});

	async function scanAllImages() {
		if (!vulnerabilityManagementEnabled || isLoading.scanningAll) return;

		const requestedScope = scopeKey();
		isLoading.scanningAll = true;
		scanProgress = { current: 0, total: 0 };

		try {
			const operationResult = await tryCatch(
				(async () => {
					// Fetch all images with a high limit to get all of them; merged, every environment's whole list.
					const environments = vulnerabilityEnvironments().filter((env) => hasPermission('vulnerabilities:scan', env.id));
					const limit = environments.length > 1 ? TABLE_PAGE_SIZE_ALL : 1000;
					const imagesResponse = await fetchAcrossEnvironments(
						environments,
						{ pagination: { page: 1, limit } },
						(id, request, config) => imageService.getImagesForEnvironment(id, request, config)
					);
					const images = imagesResponse.data ?? [];
					// Environments whose images could not be listed were not scanned.
					const skipped = imagesResponse.failedEnvironments ?? [];
					for (const { environment, message } of skipped) {
						toast.error(m.all_environments_request_failed({ environment: environment.name }), {
							id: `environment-request-failed-${environment.id}`,
							description: message
						});
					}

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
						if (destroyed || !vulnerabilityManagementEnabled || requestedScope !== scopeKey()) return;
						const batch = images.slice(i, i + BATCH_SIZE);

						await Promise.all(
							batch.map(async (image) => {
								const operationResult = await tryCatch(
									(async () => {
										const result = await vulnerabilityService.scanImage(image.id, image.environmentId);
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
					if (failed === 0 && skipped.length > 0) {
						toast.warning(m.security_scan_all_success({ count: succeeded }));
					} else if (failed === 0) {
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
	// Reads cover every shown environment, so a result is stale once the selection or the shown set changes.
	const scopeKey = () => `${currentEnvId}|${environmentStore.shownKey}`;
	const vulnerabilityManagementEnabled = $derived(vulnerabilityEnvironments().length > 0);
	const overviewEnvironmentId = $derived(vulnerabilityOverviewEnvironment()?.id ?? currentEnvId);
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
	const canScanVuln = $derived(
		vulnerabilityManagementEnabled && vulnerabilityEnvironments().some((env) => hasPermission('vulnerabilities:scan', env.id))
	);

	const actionButtons: ActionButton[] = $derived.by(() => {
		const buttons: ActionButton[] = [];
		if (canScanVuln) {
			buttons.push({
				id: 'scan-all',
				action: 'base',
				placement: 'primary',
				label: isLoading.scanningAll ? `${m.scanning()} (${scanProgress.current}/${scanProgress.total})` : m.security_scan_all(),
				onclick: scanAllImages,
				loading: isLoading.scanningAll,
				disabled: isLoading.scanningAll || isLoading.refreshing,
				icon: InspectIcon
			});
		}
		buttons.push({
			id: 'refresh',
			action: 'refresh',
			placement: 'secondary',
			iconOnly: true,
			label: m.common_refresh(),
			onclick: () => refreshAll(),
			loading: isLoading.refreshing,
			disabled: isLoading.refreshing || isLoading.scanningAll
		});
		return buttons;
	});
</script>

{#snippet vulnerabilitiesEmptyState()}
	{#if overview?.drivers.imagesTotal === 0}
		<EmptyState
			variant="plain"
			icon={ImagesIcon}
			title={m.security_no_images_to_scan()}
			description={m.security_no_images_description()}
			actionLabel={hasPermission('images:pull', currentEnvId) ? m.images_pull_image() : undefined}
			actionHref="/images"
		/>
	{:else if overview?.drivers.imagesScanned === 0}
		<EmptyState
			variant="plain"
			icon={ScanIcon}
			title={m.security_not_scanned_title()}
			description={m.security_not_scanned_description()}
			actionLabel={canScanVuln && !isLoading.scanningAll ? m.security_scan_all() : undefined}
			onAction={scanAllImages}
		/>
	{:else if !showIgnored && !fixAvailable && vulnerabilities.pagination.totalItems === 0}
		<EmptyState
			variant="plain"
			icon={ShieldCheckIcon}
			title={m.vuln_no_vulnerabilities()}
			description={m.security_no_findings_description()}
		/>
	{:else}
		<EmptyState variant="plain" title={m.common_no_results_found()} description={m.common_no_results_hint()} />
	{/if}
{/snippet}

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
				<div class="flex flex-wrap items-center justify-between gap-2">
					<TabBar items={securityTabItems} value={activeTab} onValueChange={handleTabChange} />
					{#if activeTab === 'overview' || (activeTab === 'vulnerabilities' && overview)}
						<EnvironmentDestinationSelect environments={vulnerabilityOverviewEnvironments()} />
					{/if}
				</div>
				<Tabs.Content value="overview" class="mt-4">
					{#if overview}
						<SecurityOverview {overview} environmentId={overviewEnvironmentId} onSelectVulnerability={showVulnerability} />
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
							emptyState={vulnerabilitiesEmptyState}
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
