<script lang="ts">
	import * as Card from '$lib/components/ui/card';
	import * as Tabs from '$lib/components/ui/tabs/index.js';
	import { goto } from '$app/navigation';
	import { Badge } from '$lib/components/ui/badge';
	import { format } from 'date-fns';
	import { bytes } from '$lib/utils/formatting';
	import { openConfirmDialog } from '$lib/components/confirm-dialog';
	import { handleApiResultWithCallbacks } from '$lib/utils/api';
	import { tryCatch } from '$lib/utils/api';
	import { toast } from 'svelte-sonner';
	import { onDestroy } from 'svelte';
	import { ArcaneButton } from '$lib/components/arcane-button';
	import { m } from '$lib/paraglide/messages';
	import { imageService } from '$lib/services/image-service.js';
	import { vulnerabilityService } from '$lib/services/vulnerability-service.js';
	import { activityToastOptions, extractActivityId } from '$lib/utils/activity-toast';
	import {
		startVulnerabilityScanPolling,
		stabilizeFailedVulnerabilitySummary,
		isVulnerabilityScanInProgress
	} from '$lib/utils/docker';
	import { ResourceDetailLayout, type DetailAction } from '$lib/layouts';
	import ImageAttestationsPanel from './image-attestations-panel.svelte';
	import ImageHistoryPanel from './image-history-panel.svelte';
	import ImageTagDialog from '../components/image-tag-dialog.svelte';
	import VulnerabilityScanPanel from '$lib/components/vulnerability/vulnerability-scan-panel.svelte';
	import type { VulnerabilityScanResult } from '$lib/types/environment';
	import { environmentStore } from '$lib/stores/environment.store.svelte';
	import { hasPermission } from '$lib/utils/auth';
	import { toastVulnerabilityScanStatus } from '$lib/utils/vulnerability';
	import { VolumesIcon, ClockIcon, TagIcon, CpuIcon, ShieldCheckIcon } from '$lib/icons';
	import { cn } from '$lib/utils';

	let { data } = $props();
	let { image } = $derived(data);

	let securityTab = $state('attestations');

	const currentEnvId = $derived(environmentStore.selected?.id || '0');
	const canDeleteImage = $derived(hasPermission('images:delete', currentEnvId));
	const canScanImage = $derived(hasPermission('vulnerabilities:scan', currentEnvId));
	const canTagImage = $derived(hasPermission('images:tag', currentEnvId));
	const canReadImage = $derived(hasPermission('images:read', currentEnvId));

	let isLoading = $state({
		pulling: false,
		removing: false,
		exporting: false,
		scanning: false
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
			lastScanRequestedAt = result.scanTime || new Date().toISOString();
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
		try {
			const date = new Date(image.created);
			if (isNaN(date.getTime())) return m.common_na();
			return format(date, 'PP p');
		} catch {
			return m.common_na();
		}
	});

	const imageSize = $derived.by(() => bytes.format(Number(image?.size ?? 0)) || '0 B');
	const architecture = $derived.by(() => image?.architecture || m.common_na());
	const osName = $derived.by(() => image?.os || m.common_na());
	const repoTags = $derived.by(() => image?.repoTags ?? []);
	const envVars = $derived.by(() => image?.config?.env ?? []);
	const hasTags = $derived.by(() => repoTags.length > 0);
	const hasEnv = $derived.by(() => envVars.length > 0);

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
						message: m.images_remove_failed(),
						setLoadingState: (value) => (isLoading.removing = value),
						onSuccess: async (data) => {
							toast.success(m.images_remove_success(), activityToastOptions(extractActivityId(data)));
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

<ResourceDetailLayout backUrl="/images" backLabel={m.images_title()} title={image?.repoTags?.[0] || shortId} {actions}>
	{#if image}
		<div class="space-y-6">
			{#snippet tile(label: string, value: string, opts?: { mono?: boolean; class?: string })}
				<Card.Root variant="subtle" class={opts?.class}>
					<Card.Content class="flex flex-col gap-1 p-4">
						<div class="text-muted-foreground text-xs font-semibold tracking-wide uppercase">{label}</div>
						<div class={cn('text-foreground text-sm font-medium', opts?.mono && 'font-mono break-all select-all')}>
							{value}
						</div>
					</Card.Content>
				</Card.Root>
			{/snippet}

			<div class="bg-muted/40 flex flex-wrap items-center gap-x-4 gap-y-2 rounded-lg px-4 py-3">
				<div class="text-muted-foreground flex items-center gap-1.5 text-sm">
					<VolumesIcon class="size-4 shrink-0" />
					<span>{imageSize}</span>
				</div>
				<div class="text-muted-foreground flex items-center gap-1.5 text-sm">
					<ClockIcon class="size-4 shrink-0" />
					<span>{createdDate}</span>
				</div>
				<div class="text-muted-foreground flex items-center gap-1.5 text-sm">
					<CpuIcon class="size-4 shrink-0" />
					<span>{architecture} · {osName}</span>
				</div>
			</div>

			{#if hasTags}
				<div class="flex flex-wrap items-center gap-2">
					<span class="text-muted-foreground inline-flex items-center gap-2 text-xs font-semibold tracking-wide uppercase">
						<TagIcon class="size-4" />
						{m.common_tags()}
					</span>
					{#each repoTags as tag (tag)}
						<Badge variant="secondary" class="cursor-pointer text-xs select-all" title="Click to select">
							{tag}
						</Badge>
					{/each}
				</div>
			{/if}

			<div class="grid grid-cols-1 gap-3 sm:grid-cols-2 lg:grid-cols-3">
				{@render tile(m.common_id(), image?.id || m.common_na(), { mono: true, class: 'sm:col-span-2 lg:col-span-3' })}
				{#if image?.dockerVersion}
					{@render tile(m.common_docker_version(), image.dockerVersion)}
				{/if}
				{#if image?.author}
					{@render tile(m.common_author(), image.author)}
				{/if}
				{#if image.config?.workingDir}
					{@render tile(m.common_working_dir(), image.config.workingDir, { mono: true })}
				{/if}
			</div>

			{#if hasEnv}
				<div class="space-y-4 border-t pt-6">
					<h3 class="text-sm font-medium">{m.common_environment_variables()}</h3>
					<div class="grid grid-cols-1 gap-3 sm:grid-cols-2">
						{#each envVars as env (env)}
							{#if env.includes('=')}
								{@const [key, ...valueParts] = env.split('=')}
								{@render tile(key ?? '', valueParts.join('='), { mono: true })}
							{:else}
								{@render tile('ENV_VAR', env, { mono: true })}
							{/if}
						{/each}
					</div>
				</div>
			{/if}

			<div class="space-y-4 border-t pt-6">
				<h3 class="flex items-center gap-2 text-sm font-medium">
					<ShieldCheckIcon class="size-4" />
					{m.images_details_title()}
				</h3>
				<Tabs.Root bind:value={securityTab} class="space-y-4">
					<Tabs.List>
						<Tabs.Trigger value="history">{m.images_history_title()}</Tabs.Trigger>
						<Tabs.Trigger value="attestations">{m.images_attestations_title()}</Tabs.Trigger>
						<Tabs.Trigger value="vulnerabilities">{m.vuln_title()}</Tabs.Trigger>
					</Tabs.List>
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
			</div>
		</div>
	{:else}
		<div class="py-12 text-center">
			<p class="text-muted-foreground text-lg font-medium">{m.common_not_found_title({ resource: m.images_title() })}</p>
			<ArcaneButton
				action="cancel"
				customLabel={m.common_back_to({ resource: m.images_title() })}
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
		defaultRepository={image.repoTags?.[0]?.split(':')[0] ?? ''}
		onTagged={() => goto(`/images/${image.id}`, { invalidateAll: true })}
	/>
{/if}
