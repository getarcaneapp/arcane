<script lang="ts">
	import * as Tabs from '#lib/components/ui/tabs/index.js';
	import { goto } from '$app/navigation';
	import { Badge } from '#lib/components/ui/badge';
	import { bytes, formatDateTimeShort, nowInstantString } from '#lib/utils/formatting';
	import { openConfirmDialog } from '#lib/components/confirm-dialog';
	import { handleApiResultWithCallbacks } from '#lib/utils/api';
	import { tryCatch } from '#lib/utils/api';
	import { toast } from 'svelte-sonner';
	import { onDestroy } from 'svelte';
	import { ArcaneButton } from '#lib/components/arcane-button';
	import { m } from '#lib/paraglide/messages';
	import { imageService } from '#lib/services/image-service.js';
	import { vulnerabilityService } from '#lib/services/vulnerability-service.js';
	import { activityToastOptions, extractActivityId } from '#lib/utils/activity-toast';
	import {
		startVulnerabilityScanPolling,
		stabilizeFailedVulnerabilitySummary,
		isVulnerabilityScanInProgress
	} from '#lib/utils/docker';
	import { ResourceDetailLayout, type DetailAction } from '#lib/layouts';
	import { TabBar, type TabItem } from '#lib/components/tab-bar';
	import { useUrlTab } from '#lib/hooks/use-url-tab.svelte';
	import { DetailMetaStrip, DetailSection, KeyValueCard } from '#lib/components/resource-detail';
	import ImageAttestationsPanel from './image-attestations-panel.svelte';
	import ImageHistoryPanel from './image-history-panel.svelte';
	import ImageTagDialog from '../components/image-tag-dialog.svelte';
	import VulnerabilityScanPanel from '#lib/components/vulnerability/vulnerability-scan-panel.svelte';
	import { CopyButton } from '#lib/components/ui/copy-button';
	import type { VulnerabilityScanResult } from '#lib/types/environment';
	import { environmentStore } from '#lib/stores/environment.store.svelte';
	import { hasPermission } from '#lib/utils/auth';
	import { parseImageRef } from '#lib/utils/docker';
	import { toastVulnerabilityScanStatus } from '#lib/utils/vulnerability';
	import { VolumesIcon, ClockIcon, TagIcon, CpuIcon, ImagesIcon, LayersIcon, ShieldCheckIcon, InspectIcon } from '#lib/icons';

	let { data } = $props();
	let { image } = $derived(data);

	const tabItems: TabItem[] = $derived([
		{ value: 'overview', label: m.common_overview(), icon: ImagesIcon },
		{ value: 'history', label: m.images_history_title(), icon: LayersIcon },
		{ value: 'attestations', label: m.images_attestations_title(), icon: InspectIcon },
		{ value: 'vulnerabilities', label: m.vuln_title(), icon: ShieldCheckIcon }
	]);
	const urlTab = useUrlTab({
		validTabs: () => tabItems.map((tab) => tab.value),
		defaultTab: () => 'overview'
	});
	const activeTab = $derived(urlTab.value);

	const currentEnvId = $derived(environmentStore.selected?.id || '0');
	// fallow-ignore-next-line code-duplication -- permission $derived declarations; script-level, no shared render surface
	const canDeleteImage = $derived(hasPermission('images:delete', currentEnvId));
	const canScanImage = $derived(hasPermission('vulnerabilities:scan', currentEnvId));
	const canPatchImage = $derived(hasPermission('images:patch', currentEnvId));
	const canTagImage = $derived(hasPermission('images:tag', currentEnvId));
	const canReadImage = $derived(hasPermission('images:read', currentEnvId));

	let isLoading = $state({
		pulling: false,
		removing: false,
		exporting: false,
		scanning: false,
		patching: false
	});
	let tagDialogOpen = $state(false);

	let vulnerabilityScan = $state<VulnerabilityScanResult | null>(null);
	let hasLoadedVulnerabilities = $state(false);
	let stopScanPolling: (() => void) | null = $state(null);
	let lastScanRequestedAt = $state<string | null>(null);

	// Load vulnerability scan data when image changes
	$effect(() => {
		if (image?.id && !hasLoadedVulnerabilities) {
			loadVulnerabilityScan();
		}
	});

	async function loadVulnerabilityScan() {
		if (!image?.id) return;
		try {
			const result = await vulnerabilityService.getScanResult(image.id);
			vulnerabilityScan = result;
			lastScanRequestedAt = result.scanTime || lastScanRequestedAt;
		} catch {
			// No scan data found, that's okay
			vulnerabilityScan = null;
		}
		hasLoadedVulnerabilities = true;
	}

	async function handleScanImage() {
		if (!image?.id || isLoading.scanning) return;
		isLoading.scanning = true;
		try {
			const result = await vulnerabilityService.scanImage(image.id);
			vulnerabilityScan = result;
			lastScanRequestedAt = result.scanTime || nowInstantString();
			if (isVulnerabilityScanInProgress(result.status)) {
				toastVulnerabilityScanStatus(result, { includeStarted: true });
				beginScanPolling(true);
			} else {
				toastVulnerabilityScanStatus(result);
			}
		} catch (error) {
			console.error('Failed to scan image:', error);
			toast.error(m.vuln_scan_failed());
		} finally {
			isLoading.scanning = false;
		}
	}

	async function handlePatchImage() {
		if (!image?.id || isLoading.patching) return;
		isLoading.patching = true;

		// Prefer the stored scan report when one exists; fall back to
		// patching all outdated OS packages.
		const options = vulnerabilityScan?.hasReport ? { scanId: image.id } : undefined;
		const result = await tryCatch(imageService.patchImage(image.id, options));
		handleApiResultWithCallbacks({
			result,
			message: m.images_patch_failed(),
			setLoadingState: (value) => (isLoading.patching = value),
			onSuccess: async (data) => {
				toast.info(m.images_patch_started({ patchedRef: data.patchedRef }), activityToastOptions(data.activityId));
			}
		});
	}

	function stopPolling() {
		if (stopScanPolling) {
			stopScanPolling();
			stopScanPolling = null;
		}
	}

	function beginScanPolling(showToast: boolean) {
		if (!image?.id || stopScanPolling) return;
		const cancel = startVulnerabilityScanPolling(image.id, (id) => vulnerabilityService.getScanSummary(id), {
			onUpdate: (summary) => {
				vulnerabilityScan = {
					...(vulnerabilityScan ?? {}),
					imageId: summary.imageId,
					scanTime: summary.scanTime,
					status: summary.status,
					scanPhase: summary.scanPhase,
					summary: summary.summary,
					error: summary.error
				} as VulnerabilityScanResult;
			},
			onComplete: async (summary) => {
				let resolvedSummary = summary;
				try {
					resolvedSummary = await stabilizeFailedVulnerabilitySummary(
						summary.imageId,
						summary,
						(id) => vulnerabilityService.getScanSummary(id),
						{ scanRequestedAt: lastScanRequestedAt ?? vulnerabilityScan?.scanTime }
					);
				} catch {
					// Keep original summary when stabilization check fails.
				}

				if (isVulnerabilityScanInProgress(resolvedSummary.status)) {
					vulnerabilityScan = {
						...(vulnerabilityScan ?? {}),
						imageId: resolvedSummary.imageId,
						scanTime: resolvedSummary.scanTime,
						status: resolvedSummary.status,
						scanPhase: resolvedSummary.scanPhase,
						summary: resolvedSummary.summary,
						error: resolvedSummary.error
					} as VulnerabilityScanResult;
					stopPolling();
					beginScanPolling(false);
					return;
				}

				stopPolling();
				try {
					vulnerabilityScan = await vulnerabilityService.getScanResult(resolvedSummary.imageId);
				} catch (error) {
					console.error('Failed to load scan result:', error);
					vulnerabilityScan = {
						...(vulnerabilityScan ?? {}),
						imageId: resolvedSummary.imageId,
						scanTime: resolvedSummary.scanTime,
						status: resolvedSummary.status,
						scanPhase: resolvedSummary.scanPhase,
						summary: resolvedSummary.summary,
						error: resolvedSummary.error
					} as VulnerabilityScanResult;
				}
				if (showToast) {
					toastVulnerabilityScanStatus(resolvedSummary);
				}
			},
			onError: () => {}
		});

		stopScanPolling = cancel;
	}

	$effect(() => {
		if (!lastScanRequestedAt && vulnerabilityScan?.scanTime) {
			lastScanRequestedAt = vulnerabilityScan.scanTime;
		}
		const scanning = isVulnerabilityScanInProgress(vulnerabilityScan?.status);
		if (scanning) {
			beginScanPolling(false);
		} else {
			stopPolling();
		}
	});

	onDestroy(() => {
		stopPolling();
	});

	const shortId = $derived.by(() => image?.id?.split(':')[1]?.substring(0, 12) || m.common_na());

	const createdDate = $derived.by(() => {
		if (!image?.created) return m.common_na();
		return formatDateTimeShort(image.created) || m.common_na();
	});

	const imageSize = $derived.by(() => bytes.format(Number(image?.size ?? 0)) || '0 B');
	const architecture = $derived.by(() => image?.architecture || m.common_na());
	const osName = $derived.by(() => image?.os || m.common_na());
	const repoTags = $derived.by(() => image?.repoTags ?? []);
	const pinnedRefs = $derived.by(() => image?.pinnedReferences ?? []);
	const envVars = $derived.by(() => image?.config?.env ?? []);
	const hasTags = $derived.by(() => repoTags.length > 0 && repoTags[0] !== '<none>:<none>');
	const hasPinnedRefs = $derived.by(() => pinnedRefs.length > 0);
	const hasEnv = $derived.by(() => envVars.length > 0);
	const detailTitle = $derived.by((): string => {
		if (hasTags && repoTags[0]) return repoTags[0];
		if (hasPinnedRefs && pinnedRefs[0]) return pinnedRefs[0];
		return shortId;
	});
	const pinnedRepository = $derived.by(() => (pinnedRefs[0] ? parseImageRef(pinnedRefs[0]).repo : ''));

	async function handleImageRemove(id: string) {
		openConfirmDialog({
			title: m.common_remove_title({ resource: m.resource_image() }),
			message: m.images_remove_message(),
			checkboxes: [
				{
					id: 'force',
					label: m.images_remove_force_label(),
					initialState: false
				}
			],
			confirm: {
				label: m.common_delete(),
				destructive: true,
				action: async (checkboxStates) => {
					const force = !!checkboxStates['force'];
					await handleApiResultWithCallbacks({
						result: await tryCatch(imageService.deleteImage(id, { force })),
						message: m.failed_to_remove_image(),
						setLoadingState: (value) => (isLoading.removing = value),
						onSuccess: async (data) => {
							toast.success(m.image_removed_successfully(), activityToastOptions(extractActivityId(data)));
							goto('/images');
						}
					});
				}
			}
		});
	}

	async function handleExportImage(id: string) {
		isLoading.exporting = true;
		try {
			const url = await imageService.getImageExportUrl(id);
			window.open(url, '_blank', 'noopener,noreferrer');
		} finally {
			isLoading.exporting = false;
		}
	}

	const actions: DetailAction[] = $derived.by(() => {
		const list: DetailAction[] = [];
		if (canTagImage) {
			list.push({
				id: 'tag',
				action: 'tag',
				label: m.images_tag_image(),
				onclick: () => (tagDialogOpen = true)
			});
		}
		if (canReadImage) {
			list.push({
				id: 'export',
				action: 'pull',
				label: m.images_export(),
				loading: isLoading.exporting,
				disabled: isLoading.exporting,
				onclick: () => handleExportImage(image.id)
			});
		}
		if (canScanImage) {
			list.push({
				id: 'scan',
				action: 'scan',
				label: m.vuln_scan(),
				loading: isLoading.scanning,
				disabled: isLoading.scanning,
				onclick: handleScanImage
			});
		}
		// Locally built images have no registry source to patch from; the
		// security page explains this, the header just omits the action.
		if (canPatchImage && image?.repoTags?.[0] && (image?.repoDigests?.length ?? 0) > 0) {
			list.push({
				id: 'patch',
				action: 'patch',
				label: m.images_patch(),
				loading: isLoading.patching,
				disabled: isLoading.patching,
				onclick: handlePatchImage
			});
		}
		if (canDeleteImage) {
			list.push({
				id: 'remove',
				action: 'remove',
				label: m.common_remove(),
				loading: isLoading.removing,
				disabled: isLoading.removing,
				onclick: () => handleImageRemove(image.id)
			});
		}
		return list;
	});
</script>

<ResourceDetailLayout backUrl="/images" backLabel={m.images()} title={detailTitle} {actions}>
	{#if image}
		{#snippet kvTile(label: string, value: string, opts?: { class?: string })}
			<KeyValueCard {label} cardClass={opts?.class} valueTitle={value}>
				{value}
			</KeyValueCard>
		{/snippet}

		<Tabs.Root value={activeTab} class="space-y-6">
			<TabBar items={tabItems} value={activeTab} onValueChange={(value) => urlTab.select(value)} />

			<Tabs.Content value="overview" class="space-y-6">
				<DetailMetaStrip
					items={[
						{ icon: VolumesIcon, value: imageSize },
						{ icon: ClockIcon, value: createdDate },
						{ icon: CpuIcon, value: `${architecture} · ${osName}` }
					]}
				/>

				{#if hasPinnedRefs}
					<div class="space-y-2">
						<span class="inline-flex items-center gap-2 text-xs font-semibold tracking-wide text-muted-foreground uppercase">
							<TagIcon class="size-4" />
							{m.images_pinned_references()}
						</span>
						<div class="flex flex-col gap-2">
							{#each pinnedRefs as pin (pin)}
								<div
									class="flex max-w-2xl items-center justify-between gap-2 rounded-lg border border-border/50 bg-muted/40 p-2.5"
								>
									<span class="font-mono text-xs break-all text-foreground select-all" title={m.common_click_to_select()}>
										{pin}
									</span>
									<CopyButton text={pin} size="icon" class="size-7 shrink-0" />
								</div>
							{/each}
						</div>
					</div>
				{/if}

				{#if hasTags}
					<div class="flex flex-wrap items-center gap-2">
						<span class="inline-flex items-center gap-2 text-xs font-semibold tracking-wide text-muted-foreground uppercase">
							<TagIcon class="size-4" />
							{m.common_tags()}
						</span>
						{#each repoTags as tag (tag)}
							<Badge variant="secondary" class="cursor-pointer text-xs select-all" title={m.common_click_to_select()}>
								{tag}
							</Badge>
						{/each}
					</div>
				{/if}

				<div class="grid grid-cols-1 gap-3 sm:grid-cols-2 lg:grid-cols-3">
					{@render kvTile(m.common_id(), image?.id || m.common_na(), { class: 'sm:col-span-2 lg:col-span-3' })}
					{#if image?.dockerVersion}
						{@render kvTile(m.common_docker_version(), image.dockerVersion)}
					{/if}
					{#if image?.author}
						{@render kvTile(m.common_author(), image.author)}
					{/if}
					{#if image.config?.workingDir}
						{@render kvTile(m.common_working_dir(), image.config.workingDir)}
					{/if}
				</div>

				{#if hasEnv}
					<DetailSection title={m.common_environment_variables()}>
						<div class="grid grid-cols-1 gap-3 sm:grid-cols-2">
							{#each envVars as env (env)}
								{#if env.includes('=')}
									{@const [key, ...valueParts] = env.split('=')}
									{@render kvTile(key ?? '', valueParts.join('='))}
								{:else}
									{@render kvTile('ENV_VAR', env)}
								{/if}
							{/each}
						</div>
					</DetailSection>
				{/if}
			</Tabs.Content>

			<Tabs.Content value="history">
				<ImageHistoryPanel imageId={image.id} />
			</Tabs.Content>
			<Tabs.Content value="attestations">
				<ImageAttestationsPanel {image} />
			</Tabs.Content>
			<Tabs.Content value="vulnerabilities">
				<VulnerabilityScanPanel scan={vulnerabilityScan} isScanning={isLoading.scanning} onScan={handleScanImage} />
			</Tabs.Content>
		</Tabs.Root>
	{:else}
		<div class="py-12 text-center">
			<p class="text-lg font-medium text-muted-foreground">{m.common_not_found_title({ resource: m.images() })}</p>
			<ArcaneButton
				action="cancel"
				customLabel={m.common_back_to({ resource: m.images() })}
				onclick={() => goto('/images')}
				size="sm"
				class="mt-4"
			/>
		</div>
	{/if}
</ResourceDetailLayout>

{#if image && tagDialogOpen}
	<ImageTagDialog
		bind:open={tagDialogOpen}
		imageId={image.id}
		defaultRepository={image.repoTags?.[0]?.split(':')[0] ??
			(pinnedRepository || (image.repo && image.repo !== '<none>' ? image.repo : ''))}
		onTagged={() => goto(`/images/${image.id}`, { refreshAll: true })}
	/>
{/if}
