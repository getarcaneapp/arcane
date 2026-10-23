<script lang="ts">
	import { createQuery, useQueryClient } from '@tanstack/svelte-query';
	import { onMount, onDestroy, untrack } from 'svelte';
	import { toast } from 'svelte-sonner';

	import { ArcaneButton } from '#lib/components/arcane-button/index.js';
	import FeatureDisabled from '#lib/components/features/feature-disabled.svelte';
	import { EmptyState } from '#lib/components/states/index.js';
	import { TabBar, type TabItem } from '#lib/components/tab-bar/index.js';
	import * as Alert from '#lib/components/ui/alert/index.js';
	import * as Tabs from '#lib/components/ui/tabs/index.js';
	import { BULK_FETCH_MAX_ITEMS, BULK_FETCH_PAGE_SIZE } from '#lib/constants/table-pagination.js';
	import { useUrlTab } from '#lib/hooks/use-url-tab.svelte.js';
	import {
		ActivityIcon,
		AlertTriangleIcon,
		ImagesIcon,
		InspectIcon,
		ScanIcon,
		ShieldAlertIcon,
		ShieldCheckIcon
	} from '#lib/icons/index.js';
	import { ResourcePageLayout, type ActionButton } from '#lib/layouts/index.js';
	import { m } from '#lib/paraglide/messages.js';
	import { queryKeys } from '#lib/query/query-keys.js';
	import { imageService } from '#lib/services/image-service.js';
	import { vulnerabilityService } from '#lib/services/vulnerability-service.js';
	import { activityStore } from '#lib/stores/activity.store.svelte.js';
	import { environmentStore } from '#lib/stores/environment.store.svelte.js';
	import { featureStore } from '#lib/stores/features.store.svelte.js';
	import type { Paginated, SearchPaginationSortRequest } from '#lib/types/shared.js';
	import { extractApiErrorMessage, parallelRefresh } from '#lib/utils/api.js';
	import { hasPermission } from '#lib/utils/auth.js';
	import {
		destinationEnvironmentId,
		fetchAcrossEnvironments,
		hasShownPermission,
		withFailedEnvironments
	} from '#lib/utils/environments.js';
	import { tryCatch } from '#lib/utils/try-catch.js';
	import {
		fetchRiskOverview,
		getSeveritySegments,
		mapVulnerabilityPage,
		mapVulnerabilityRequest,
		vulnerabilityEnvironments,
		withVulnerabilityToggles,
		type RiskOverview,
		type VulnerabilityRow
	} from '#lib/utils/vulnerability.js';

	import SecurityOverview from './components/security-overview.svelte';
	import SecurityPatchTable, { fetchPatchTargets, type PatchTargetPage } from './components/security-patch-table.svelte';
	import SecurityVulnerabilityTable from './components/security-vulnerability-table.svelte';

	let { data } = $props();
	const queryClient = useQueryClient();
	const scope = $derived(environmentStore.shownKey);
	const emptyPage = () => ({
		data: [],
		pagination: { totalPages: 0, totalItems: 0, currentPage: 1, itemsPerPage: 20 }
	});

	const vulnerabilityManagementEnabled = $derived(vulnerabilityEnvironments().length > 0);
	const overviewQuery = createQuery(() => ({
		queryKey: queryKeys.vulnerabilities.overviewByEnvironment(environmentStore.shownKey),
		queryFn: fetchRiskOverview,
		enabled: vulnerabilityManagementEnabled
	}));
	const overview = $derived<RiskOverview | null>(overviewQuery.data ?? null);

	let requestOptions = $derived<SearchPaginationSortRequest>(data.vulnerabilityRequestOptions);
	let selectedVulnerabilityIds = $state<string[]>([]);
	let showIgnored = $state(false);
	let fixAvailable = $state(false);

	function vulnerabilityRequest(options: SearchPaginationSortRequest): SearchPaginationSortRequest {
		return mapVulnerabilityRequest(withVulnerabilityToggles(options, { showIgnored, fixAvailable }));
	}

	async function fetchVulnerabilities(request: SearchPaginationSortRequest): Promise<Paginated<VulnerabilityRow>> {
		return mapVulnerabilityPage(
			await fetchAcrossEnvironments(vulnerabilityEnvironments(), request, (id, request, config) =>
				vulnerabilityService.getAllVulnerabilitiesForEnvironment(id, request, config)
			),
			request
		);
	}

	const vulnerabilitiesQuery = createQuery(() => {
		const queryScope = scope;
		const request = vulnerabilityRequest(requestOptions);
		return {
			queryKey: queryKeys.vulnerabilities.allByEnvironment(queryScope, request),
			queryFn: () => fetchVulnerabilities(request),
			placeholderData: (previous, query) => (query?.queryKey[1] === queryScope ? previous : undefined),
			enabled: vulnerabilityManagementEnabled
		};
	});
	const vulnerabilities = $derived<Paginated<VulnerabilityRow>>(vulnerabilitiesQuery.data ?? emptyPage());

	async function loadVulnerabilities(options: SearchPaginationSortRequest, requestedScope = scope) {
		requestOptions = options;
		const request = vulnerabilityRequest(options);
		return queryClient.fetchQuery({
			queryKey: queryKeys.vulnerabilities.allByEnvironment(requestedScope, request),
			queryFn: () => fetchVulnerabilities(request),
			staleTime: 0
		});
	}

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
	let destroyed = false;

	let patchRequestOptions = $derived<SearchPaginationSortRequest>(data.patchRequestOptions);
	const patchTargetsQuery = createQuery(() => {
		const queryScope = scope;
		const options = patchRequestOptions;
		return {
			queryKey: queryKeys.images.patchTargets(queryScope, options),
			queryFn: () => fetchPatchTargets(options),
			placeholderData: (previous, query) => (query?.queryKey[1] === queryScope ? previous : undefined),
			enabled: vulnerabilityManagementEnabled
		};
	});
	const patchTargets = $derived<PatchTargetPage>(patchTargetsQuery.data ?? emptyPage());

	async function loadPatchTargets(options: SearchPaginationSortRequest, requestedScope = scope) {
		patchRequestOptions = options;
		return queryClient.fetchQuery({
			queryKey: queryKeys.images.patchTargets(requestedScope, options),
			queryFn: () => fetchPatchTargets(options),
			staleTime: 0
		});
	}

	async function loadPatches() {
		if (!vulnerabilityManagementEnabled) return;
		const result = await patchTargetsQuery.refetch();
		if (result.error) {
			console.error('Failed to load image patch targets:', result.error);
			toast.error(m.common_refresh_failed({ resource: m.patches() }));
		}
	}

	const severityItems = $derived(getSeveritySegments(overview?.summary));

	async function refreshOverview() {
		if (!vulnerabilityManagementEnabled) return;
		const result = await overviewQuery.refetch();
		if (result.error) {
			toast.error(m.common_refresh_failed({ resource: m.security() }), { description: extractApiErrorMessage(result.error) });
		}
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
		const requestedScope = scopeKey();
		selectedVulnerabilityIds = [];
		await Promise.all(environmentStore.shown.map((env) => featureStore.refresh(env.id)));
		if (destroyed || !vulnerabilityManagementEnabled || requestedScope !== scopeKey()) return;
		const overviewRefresh = refreshOverview();
		await parallelRefresh(
			{
				vulnerabilities: {
					fetch: () =>
						reset
							? queryClient.resetQueries(
									{
										queryKey: queryKeys.vulnerabilities.allByEnvironment(requestedScope, vulnerabilityRequest(requestOptions)),
										exact: true
									},
									{ throwOnError: true }
								)
							: vulnerabilitiesQuery.refetch({ throwOnError: true }),
					onSuccess: () => {},
					errorMessage: m.common_refresh_failed({ resource: m.vuln_title() })
				},
				patches: {
					fetch: () =>
						reset
							? queryClient.resetQueries(
									{ queryKey: queryKeys.images.patchTargets(requestedScope, patchRequestOptions), exact: true },
									{ throwOnError: true }
								)
							: patchTargetsQuery.refetch({ throwOnError: true }),
					onSuccess: () => {},
					errorMessage: m.common_refresh_failed({ resource: m.patches() })
				}
			},
			(v) => (isLoading.refreshing = v)
		);
		await overviewRefresh;
	}

	function handleTabChange(value: string) {
		urlTab.select(value);
		// Ignores made in the table change the overview.
		if (value === 'overview') void refreshOverview();
	}

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
							environmentIds.includes(activity.sourceEnvironmentId || activity.environmentId) &&
							(activity.type === 'image_patch' || activity.type === 'vulnerability_scan') &&
							(activity.status === 'queued' || activity.status === 'running')
					)
					.map((activity) => [activity.id, activity.type] as const)
			);
			const scope = environmentStore.shownKey;
			const finished = observedScope === scope ? [...activeActivities].filter(([id]) => !active.has(id)) : [];
			observedScope = scope;
			activeActivities = active;
			if (finished.some(([, type]) => type === 'vulnerability_scan')) void refreshAll();
			else if (finished.length > 0 && environmentIds.some((id) => hasPermission('images:read', id))) void loadPatches();
		});
	});

	onDestroy(() => {
		destroyed = true;
	});

	async function scanAllImages() {
		if (!vulnerabilityManagementEnabled || isLoading.scanningAll) return;

		const requestedScope = scopeKey();
		isLoading.scanningAll = true;
		scanProgress = { current: 0, total: 0 };

		try {
			const operationResult = await tryCatch(
				(async () => {
					// Walk the merged image list in fixed-size pages, up to a total cap.
					const environments = vulnerabilityEnvironments().filter((env) => hasPermission('vulnerabilities:scan', env.id));
					const fetchImagesPage = (page: number) =>
						fetchAcrossEnvironments(environments, { pagination: { page, limit: BULK_FETCH_PAGE_SIZE } }, (id, request, config) =>
							imageService.getImagesForEnvironment(id, request, config)
						);
					const firstPage = await fetchImagesPage(1);
					const imagePages = [firstPage];
					for (
						let page = 2;
						page <= firstPage.pagination.totalPages && imagePages.length * BULK_FETCH_PAGE_SIZE < BULK_FETCH_MAX_ITEMS;
						page++
					) {
						imagePages.push(await fetchImagesPage(page));
					}
					const images = imagePages.flatMap((imagePage) => imagePage.data ?? []);
					// Environments whose images could not be listed were not scanned.
					const skipped =
						withFailedEnvironments(firstPage, ...imagePages.slice(1).map((imagePage) => imagePage.failedEnvironments))
							.failedEnvironments ?? [];
					for (const { environment, message } of skipped) {
						toast.error(m.all_environments_request_failed({ environment: environment.name }), {
							id: `environment-request-failed-${environment.id}`,
							description: message
						});
					}

					if (firstPage.pagination.totalItems > images.length) {
						toast.warning(m.security_scan_all_capped({ count: images.length, total: firstPage.pagination.totalItems }));
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

					await refreshAll();
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

	const scopeKey = () => environmentStore.shownKey;
	let wasFeatureEnabled = true;
	$effect(() => {
		const enabled = vulnerabilityManagementEnabled;
		// Disabled content is hidden by the template; re-enabling reloads it.
		if (enabled && !wasFeatureEnabled) {
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
			actionLabel={hasShownPermission('images:pull') ? m.images_pull_image() : undefined}
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
			<FeatureDisabled environmentId={destinationEnvironmentId('', 'vulnerabilities:read')} />
		{:else}
			<Tabs.Root value={activeTab}>
				<div class="flex flex-wrap items-center justify-between gap-2">
					<TabBar items={securityTabItems} value={activeTab} onValueChange={handleTabChange} />
				</div>
				<Tabs.Content value="overview" class="mt-4">
					{#if overview}
						<SecurityOverview {overview} onRetry={refreshOverview} onSelectVulnerability={showVulnerability} />
					{:else if overviewQuery.error}
						<Alert.Root variant="warning-subtle" size="sm" icon={AlertTriangleIcon}>
							<div class="flex flex-wrap items-center justify-between gap-2">
								<Alert.Description>{extractApiErrorMessage(overviewQuery.error)}</Alert.Description>
								<ArcaneButton action="refresh" size="sm" customLabel={m.common_retry()} onClickPromise={refreshOverview} />
							</div>
						</Alert.Root>
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
							bind:vulnerabilities={
								() => vulnerabilities,
								(value) =>
									queryClient.setQueryData(
										queryKeys.vulnerabilities.allByEnvironment(scope, vulnerabilityRequest(requestOptions)),
										value
									)
							}
							bind:requestOptions
							bind:selectedIds={selectedVulnerabilityIds}
							{showIgnored}
							onToggleIgnored={toggleIgnored}
							{fixAvailable}
							onToggleFixAvailable={toggleFixAvailable}
							onRefreshData={(options) => loadVulnerabilities(options)}
						/>
					</div>
				</Tabs.Content>
				<Tabs.Content value="patches" class="mt-4">
					{#if patchTargetsQuery.error}
						<Alert.Root variant="warning-subtle" size="sm" icon={AlertTriangleIcon} class="mb-3">
							<div class="flex flex-wrap items-center justify-between gap-2">
								<Alert.Description>{extractApiErrorMessage(patchTargetsQuery.error)}</Alert.Description>
								<ArcaneButton action="refresh" size="sm" customLabel={m.common_retry()} onClickPromise={loadPatches} />
							</div>
						</Alert.Root>
					{/if}
					<div class="rounded-xl border border-border/60">
						<SecurityPatchTable
							bind:targets={
								() => patchTargets,
								(value) => queryClient.setQueryData(queryKeys.images.patchTargets(scope, patchRequestOptions), value)
							}
							bind:requestOptions={patchRequestOptions}
							onRefreshData={(options) => loadPatchTargets(options)}
						/>
					</div>
				</Tabs.Content>
			</Tabs.Root>
		{/if}
	{/snippet}
</ResourcePageLayout>
