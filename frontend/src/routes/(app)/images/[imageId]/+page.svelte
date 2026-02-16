<script lang="ts">
	import * as Card from '$lib/components/ui/card';
	import { goto } from '$app/navigation';
	import { Badge } from '$lib/components/ui/badge';
	import { format } from 'date-fns';
	import bytes from '$lib/utils/bytes';
	import { openConfirmDialog } from '$lib/components/confirm-dialog';
	import { handleApiResultWithCallbacks } from '$lib/utils/api.util';
	import { tryCatch } from '$lib/utils/try-catch';
	import { toast } from 'svelte-sonner';
	import { onDestroy } from 'svelte';
	import { ArcaneButton } from '$lib/components/arcane-button';
	import { m } from '$lib/paraglide/messages';
	import { imageService } from '$lib/services/image-service.js';
	import { vulnerabilityService } from '$lib/services/vulnerability-service.js';
	import {
		startVulnerabilityScanPolling,
		stabilizeFailedVulnerabilitySummary,
		isVulnerabilityScanInProgress
	} from '$lib/utils/vulnerability-scan.util';
	import { ResourceDetailLayout, type DetailAction } from '$lib/layouts';
	import VulnerabilityScanPanel from '$lib/components/vulnerability/vulnerability-scan-panel.svelte';
	import type { VulnerabilityScanResult } from '$lib/types/vulnerability.type';
	import { VolumesIcon, ClockIcon, TagIcon, LayersIcon, CpuIcon, InfoIcon, SettingsIcon, HashIcon } from '$lib/icons';

	let { data } = $props();
	let { image } = $derived(data);

	let isLoading = $state({
		pulling: false,
		removing: false,
		refreshing: false,
		scanning: false
	});

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
			if (result.status === 'completed') {
				toast.success(m.vuln_scan_completed());
			} else if (result.status === 'failed') {
				toast.error(result.error || m.vuln_scan_failed());
			} else {
				beginScanPolling(true);
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
					if (resolvedSummary.status === 'completed') {
						toast.success(m.vuln_scan_completed());
					} else {
						toast.error(resolvedSummary.error || m.vuln_scan_failed());
					}
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
			confirm: {
				label: m.common_delete(),
				destructive: true,
				action: async () => {
					await handleApiResultWithCallbacks({
						result: await tryCatch(imageService.deleteImage(id)),
						message: m.images_remove_failed(),
						setLoadingState: (value) => (isLoading.removing = value),
						onSuccess: async () => {
							toast.success(m.images_remove_success());
							goto('/images');
						}
					});
				}
			}
		});
	}

	const actions: DetailAction[] = $derived([
		{
			id: 'scan',
			action: 'base',
			label: m.vuln_scan(),
			loading: isLoading.scanning,
			disabled: isLoading.scanning,
			onclick: handleScanImage
		},
		{
			id: 'remove',
			action: 'remove',
			label: m.common_remove(),
			loading: isLoading.removing,
			disabled: isLoading.removing,
			onclick: () => handleImageRemove(image.id)
		}
	]);
</script>

<ResourceDetailLayout
	backUrl="/images"
	backLabel={m.images_title()}
	title={image?.repoTags?.[0] || shortId}
	subtitle={shortId}
	{actions}
>
	{#if image}
		<div class="space-y-6">
			{#if hasTags}
				<div class="border-border/60 bg-muted/30 rounded-xl border px-4 py-3">
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
				</div>
			{/if}

			<Card.Root>
				<Card.Header icon={InfoIcon}>
					<div class="flex flex-col space-y-1.5">
						<Card.Title>{m.common_details_title({ resource: m.resource_image_cap() })}</Card.Title>
						<Card.Description>{m.common_details_description({ resource: m.resource_image() })}</Card.Description>
					</div>
				</Card.Header>
				<Card.Content class="p-5">
					<div class="grid gap-3 sm:grid-cols-2">
						<div class="border-border/60 bg-muted/30 rounded-2xl border p-4 sm:col-span-2">
							<div class="text-muted-foreground flex items-center gap-2 text-xs font-semibold tracking-wide uppercase">
								<HashIcon class="text-muted-foreground size-4" />
								{m.common_id()}
							</div>
							<p
								class="mt-2 cursor-pointer font-mono text-xs font-semibold break-all select-all sm:text-sm"
								title="Click to select"
							>
								{image?.id || m.common_na()}
							</p>
						</div>

						<div class="border-border/60 bg-muted/30 rounded-xl border p-3">
							<div class="text-muted-foreground flex items-center gap-2 text-xs font-semibold">
								<VolumesIcon class="size-4 text-blue-500" />
								{m.common_size()}
							</div>
							<p class="mt-2 cursor-pointer text-sm font-semibold select-all" title="Click to select">{imageSize}</p>
						</div>

						<div class="border-border/60 bg-muted/30 rounded-xl border p-3">
							<div class="text-muted-foreground flex items-center gap-2 text-xs font-semibold">
								<ClockIcon class="size-4 text-green-500" />
								{m.common_created()}
							</div>
							<p class="mt-2 cursor-pointer text-sm font-semibold select-all" title="Click to select">{createdDate}</p>
						</div>

						<div class="border-border/60 bg-muted/30 rounded-xl border p-3">
							<div class="text-muted-foreground flex items-center gap-2 text-xs font-semibold">
								<CpuIcon class="size-4 text-orange-500" />
								{m.common_architecture()}
							</div>
							<p class="mt-2 cursor-pointer text-sm font-semibold select-all" title="Click to select">{architecture}</p>
						</div>

						<div class="border-border/60 bg-muted/30 rounded-xl border p-3">
							<div class="text-muted-foreground flex items-center gap-2 text-xs font-semibold">
								<LayersIcon class="size-4 text-indigo-500" />
								{m.images_os()}
							</div>
							<p class="mt-2 cursor-pointer text-sm font-semibold select-all" title="Click to select">{osName}</p>
						</div>

						{#if image?.dockerVersion}
							<div class="border-border/60 bg-muted/30 rounded-xl border p-3">
								<div class="text-muted-foreground flex items-center gap-2 text-xs font-semibold">
									<InfoIcon class="size-4 text-purple-500" />
									{m.common_docker_version()}
								</div>
								<p class="mt-2 cursor-pointer text-sm font-semibold select-all" title="Click to select">
									{image.dockerVersion}
								</p>
							</div>
						{/if}

						{#if image?.author}
							<div class="border-border/60 bg-muted/30 rounded-xl border p-3">
								<div class="text-muted-foreground flex items-center gap-2 text-xs font-semibold">
									<InfoIcon class="size-4 text-pink-500" />
									{m.common_author()}
								</div>
								<p class="mt-2 cursor-pointer text-sm font-semibold break-all select-all" title="Click to select">
									{image.author}
								</p>
							</div>
						{/if}

						{#if image.config?.workingDir}
							<div class="border-border/60 bg-muted/30 rounded-xl border p-3 sm:col-span-2">
								<div class="text-muted-foreground flex items-center gap-2 text-xs font-semibold">
									<InfoIcon class="size-4 text-amber-500" />
									{m.common_working_dir()}
								</div>
								<p
									class="mt-2 cursor-pointer font-mono text-xs font-semibold break-all select-all sm:text-sm"
									title="Click to select"
								>
									{image.config.workingDir}
								</p>
							</div>
						{/if}
					</div>

					{#if hasEnv}
						<div class="border-border/60 bg-muted/30 mt-4 rounded-2xl border p-4">
							<div class="flex flex-wrap items-start justify-between gap-3">
								<div>
									<div class="text-muted-foreground flex items-center gap-2 text-xs font-semibold tracking-wide uppercase">
										<SettingsIcon class="size-4" />
										{m.common_environment_variables()}
									</div>
								</div>
							</div>
							<div class="mt-4 grid grid-cols-1 gap-2 sm:grid-cols-2">
								{#each envVars as env (env)}
									{#if env.includes('=')}
										{@const [key, ...valueParts] = env.split('=')}
										{@const value = valueParts.join('=')}
										<div class="border-border/50 bg-muted/20 rounded-lg border px-3 py-2">
											<div class="text-muted-foreground text-[11px] font-semibold tracking-wide break-all uppercase">
												{key}
											</div>
											<div
												class="text-foreground mt-1 cursor-pointer font-mono text-xs font-medium break-all select-all"
												title="Click to select"
											>
												{value}
											</div>
										</div>
									{:else}
										<div class="border-border/50 bg-muted/20 rounded-lg border px-3 py-2">
											<div class="text-muted-foreground text-[11px] font-semibold tracking-wide uppercase">ENV_VAR</div>
											<div
												class="text-foreground mt-1 cursor-pointer font-mono text-xs font-medium break-all select-all"
												title="Click to select"
											>
												{env}
											</div>
										</div>
									{/if}
								{/each}
							</div>
						</div>
					{/if}
				</Card.Content>
			</Card.Root>

			<VulnerabilityScanPanel scan={vulnerabilityScan} isScanning={isLoading.scanning} onScan={handleScanImage} />
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
