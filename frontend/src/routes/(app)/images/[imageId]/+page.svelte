<script lang="ts">
	import * as Card from '$lib/components/ui/card';
	import { goto } from '$app/navigation';
	import { Badge } from '$lib/components/ui/badge';
	import { format } from 'date-fns';
	import bytes from 'bytes';
	import { openConfirmDialog } from '$lib/components/confirm-dialog';
	import { handleApiResultWithCallbacks } from '$lib/utils/api.util';
	import { tryCatch } from '$lib/utils/try-catch';
	import { toast } from 'svelte-sonner';
	import { ArcaneButton } from '$lib/components/arcane-button';
	import { m } from '$lib/paraglide/messages';
	import { imageService } from '$lib/services/image-service.js';
	import { vulnerabilityService } from '$lib/services/vulnerability-service.js';
	import { ResourceDetailLayout, type DetailAction } from '$lib/layouts';
	import StatusBadge from '$lib/components/badges/status-badge.svelte';
	import { Spinner } from '$lib/components/ui/spinner';
	import type { VulnerabilityScanResult, Vulnerability as VulnType } from '$lib/types/vulnerability.type';
	import { VolumesIcon, ClockIcon, TagIcon, LayersIcon, CpuIcon, InfoIcon, SettingsIcon, HashIcon, ShieldCheckIcon, ShieldAlertIcon, ScanIcon } from '$lib/icons';

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
			if (result.status === 'completed') {
				toast.success(m.vuln_scan_completed());
			} else if (result.status === 'failed') {
				toast.error(result.error || m.vuln_scan_failed());
			}
		} catch (error) {
			console.error('Failed to scan image:', error);
			toast.error(m.vuln_scan_failed());
		} finally {
			isLoading.scanning = false;
		}
	}

	const shortId = $derived(() => image?.id?.split(':')[1]?.substring(0, 12) || m.common_na());

	const createdDate = $derived(() => {
		if (!image?.created) return m.common_na();
		try {
			const date = new Date(image.created);
			if (isNaN(date.getTime())) return m.common_na();
			return format(date, 'PP p');
		} catch {
			return m.common_na();
		}
	});

	const imageSize = $derived(() => bytes.format(image?.size || 0) || '0 B');
	const architecture = $derived(() => image?.architecture || m.common_na());
	const osName = $derived(() => image?.os || m.common_na());

	// Determine the appropriate icon for the vulnerability scan header
	const vulnerabilityHeaderIcon = $derived.by(() => {
		const isClean = vulnerabilityScan?.status === 'completed' && (vulnerabilityScan?.summary?.total ?? 0) === 0;
		return isClean ? ShieldCheckIcon : ShieldAlertIcon;
	});

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
	title={image?.repoTags?.[0] || shortId()}
	subtitle={shortId()}
	{actions}
>
	{#snippet badges()}
		{#if image?.architecture}
			<StatusBadge variant="blue" text={image.architecture} />
		{/if}
		{#if image?.os}
			<StatusBadge variant="purple" text={image.os} />
		{/if}
		<StatusBadge variant="gray" text={imageSize()} />
	{/snippet}

	{#if image}
		<div class="space-y-6">
			<Card.Root>
				<Card.Header icon={InfoIcon}>
					<div class="flex flex-col space-y-1.5">
						<Card.Title>{m.common_details_title({ resource: m.resource_image_cap() })}</Card.Title>
						<Card.Description>{m.common_details_description({ resource: m.resource_image() })}</Card.Description>
					</div>
				</Card.Header>
				<Card.Content class="p-4">
					<div class="grid grid-cols-1 gap-x-4 gap-y-6 sm:grid-cols-2 lg:grid-cols-3 xl:grid-cols-4 2xl:grid-cols-6">
						<div class="flex items-start gap-3">
							<div class="flex size-10 shrink-0 items-center justify-center rounded-full bg-gray-500/10 p-2">
								<HashIcon class="size-5 text-gray-500" />
							</div>
							<div class="min-w-0 flex-1">
								<p class="text-muted-foreground text-sm font-medium">{m.common_id()}</p>
								<p
									class="mt-1 cursor-pointer font-mono text-xs font-semibold break-all select-all sm:text-sm"
									title="Click to select"
								>
									{image?.id || m.common_na()}
								</p>
							</div>
						</div>

						<div class="flex items-start gap-3">
							<div class="flex size-10 shrink-0 items-center justify-center rounded-full bg-blue-500/10 p-2">
								<VolumesIcon class="size-5 text-blue-500" />
							</div>
							<div class="min-w-0 flex-1">
								<p class="text-muted-foreground text-sm font-medium">{m.common_size()}</p>
								<p class="mt-1 cursor-pointer text-sm font-semibold select-all sm:text-base" title="Click to select">
									{imageSize()}
								</p>
							</div>
						</div>

						<div class="flex items-start gap-3">
							<div class="flex size-10 shrink-0 items-center justify-center rounded-full bg-green-500/10 p-2">
								<ClockIcon class="size-5 text-green-500" />
							</div>
							<div class="min-w-0 flex-1">
								<p class="text-muted-foreground text-sm font-medium">{m.common_created()}</p>
								<p class="mt-1 cursor-pointer text-sm font-semibold select-all sm:text-base" title="Click to select">
									{createdDate()}
								</p>
							</div>
						</div>

						<div class="flex items-start gap-3">
							<div class="flex size-10 shrink-0 items-center justify-center rounded-full bg-orange-500/10 p-2">
								<CpuIcon class="size-5 text-orange-500" />
							</div>
							<div class="min-w-0 flex-1">
								<p class="text-muted-foreground text-sm font-medium">{m.common_architecture()}</p>
								<p class="mt-1 cursor-pointer text-sm font-semibold select-all sm:text-base" title="Click to select">
									{architecture()}
								</p>
							</div>
						</div>

						<div class="flex items-start gap-3">
							<div class="flex size-10 shrink-0 items-center justify-center rounded-full bg-indigo-500/10 p-2">
								<LayersIcon class="size-5 text-indigo-500" />
							</div>
							<div class="min-w-0 flex-1">
								<p class="text-muted-foreground text-sm font-medium">{m.images_os()}</p>
								<p class="mt-1 cursor-pointer text-sm font-semibold select-all sm:text-base" title="Click to select">
									{osName()}
								</p>
							</div>
						</div>

						{#if image?.dockerVersion}
							<div class="flex items-start gap-3">
								<div class="flex size-10 shrink-0 items-center justify-center rounded-full bg-purple-500/10 p-2">
									<InfoIcon class="size-5 text-purple-500" />
								</div>
								<div class="min-w-0 flex-1">
									<p class="text-muted-foreground text-sm font-medium">{m.common_docker_version()}</p>
									<p class="mt-1 cursor-pointer text-sm font-semibold select-all sm:text-base" title="Click to select">
										{image.dockerVersion}
									</p>
								</div>
							</div>
						{/if}

						{#if image?.author}
							<div class="flex items-start gap-3">
								<div class="flex size-10 shrink-0 items-center justify-center rounded-full bg-pink-500/10 p-2">
									<InfoIcon class="size-5 text-pink-500" />
								</div>
								<div class="min-w-0 flex-1">
									<p class="text-muted-foreground text-sm font-medium">{m.common_author()}</p>
									<p class="mt-1 cursor-pointer text-sm font-semibold break-all select-all sm:text-base" title="Click to select">
										{image.author}
									</p>
								</div>
							</div>
						{/if}

						{#if image.config?.workingDir}
							<div class="flex items-start gap-3">
								<div class="flex size-10 shrink-0 items-center justify-center rounded-full bg-amber-500/10 p-2">
									<InfoIcon class="size-5 text-amber-500" />
								</div>
								<div class="min-w-0 flex-1">
									<p class="text-muted-foreground text-sm font-medium">{m.common_working_dir()}</p>
									<p
										class="mt-1 cursor-pointer font-mono text-xs font-semibold break-all select-all sm:text-sm"
										title="Click to select"
									>
										{image.config.workingDir}
									</p>
								</div>
							</div>
						{/if}
					</div>
				</Card.Content>
			</Card.Root>

			<!-- Vulnerability Scan Section -->
			<Card.Root>
				<Card.Header icon={vulnerabilityHeaderIcon}>
					<div class="flex flex-col space-y-1.5">
						<Card.Title>{m.vuln_title()}</Card.Title>
						<Card.Description>
							{#if vulnerabilityScan?.status === 'completed'}
								{m.vuln_scan_time()}: {format(new Date(vulnerabilityScan.scanTime), 'PP p')}
							{:else if isLoading.scanning}
								{m.vuln_scanning()}
							{:else}
								{m.vuln_no_scan()}
							{/if}
						</Card.Description>
					</div>
				</Card.Header>
				<Card.Content class="p-4">
					{#if isLoading.scanning}
						<div class="flex items-center justify-center py-8">
							<Spinner class="size-8" />
							<span class="ml-3 text-muted-foreground">{m.vuln_scanning()}</span>
						</div>
					{:else if vulnerabilityScan?.status === 'completed' && vulnerabilityScan.summary}
						{@const summary = vulnerabilityScan.summary}
						{#if summary.total === 0}
							<div class="flex items-center gap-3 rounded-lg bg-green-50 p-4 dark:bg-green-950/20">
								<ShieldCheckIcon class="size-8 text-green-500" />
								<div>
									<p class="font-medium text-green-800 dark:text-green-200">{m.vuln_clean()}</p>
									<p class="text-sm text-green-600 dark:text-green-400">{m.vuln_no_vulnerabilities()}</p>
								</div>
							</div>
						{:else}
							<!-- Severity Summary Cards -->
							<div class="mb-6 grid grid-cols-2 gap-3 sm:grid-cols-5">
								{#if summary.critical > 0}
									<div class="rounded-lg border border-red-200 bg-red-50 p-3 text-center dark:border-red-800 dark:bg-red-950/20">
										<div class="text-2xl font-bold text-red-600 dark:text-red-400">{summary.critical}</div>
										<div class="text-xs font-medium text-red-800 dark:text-red-300">{m.vuln_severity_critical()}</div>
									</div>
								{/if}
								{#if summary.high > 0}
									<div class="rounded-lg border border-orange-200 bg-orange-50 p-3 text-center dark:border-orange-800 dark:bg-orange-950/20">
										<div class="text-2xl font-bold text-orange-600 dark:text-orange-400">{summary.high}</div>
										<div class="text-xs font-medium text-orange-800 dark:text-orange-300">{m.vuln_severity_high()}</div>
									</div>
								{/if}
								{#if summary.medium > 0}
									<div class="rounded-lg border border-amber-200 bg-amber-50 p-3 text-center dark:border-amber-800 dark:bg-amber-950/20">
										<div class="text-2xl font-bold text-amber-600 dark:text-amber-400">{summary.medium}</div>
										<div class="text-xs font-medium text-amber-800 dark:text-amber-300">{m.vuln_severity_medium()}</div>
									</div>
								{/if}
								{#if summary.low > 0}
									<div class="rounded-lg border border-yellow-200 bg-yellow-50 p-3 text-center dark:border-yellow-800 dark:bg-yellow-950/20">
										<div class="text-2xl font-bold text-yellow-600 dark:text-yellow-400">{summary.low}</div>
										<div class="text-xs font-medium text-yellow-800 dark:text-yellow-300">{m.vuln_severity_low()}</div>
									</div>
								{/if}
								<div class="rounded-lg border border-gray-200 bg-gray-50 p-3 text-center dark:border-gray-700 dark:bg-gray-950/20">
									<div class="text-2xl font-bold text-gray-600 dark:text-gray-400">{summary.total}</div>
									<div class="text-xs font-medium text-gray-800 dark:text-gray-300">{m.common_total()}</div>
								</div>
							</div>

							<!-- Vulnerability List -->
							{#if vulnerabilityScan.vulnerabilities && vulnerabilityScan.vulnerabilities.length > 0}
								<div class="space-y-2">
									<h4 class="text-sm font-medium text-muted-foreground">{m.vuln_details()}</h4>
									<div class="max-h-96 overflow-y-auto rounded-lg border">
										<table class="w-full text-sm">
											<thead class="sticky top-0 bg-muted">
												<tr>
													<th class="p-2 text-left font-medium">CVE</th>
													<th class="p-2 text-left font-medium">{m.vuln_package()}</th>
													<th class="p-2 text-left font-medium">{m.common_status()}</th>
													<th class="p-2 text-left font-medium">{m.vuln_installed_version()}</th>
													<th class="p-2 text-left font-medium">{m.vuln_fixed_version()}</th>
												</tr>
											</thead>
											<tbody>
												{#each vulnerabilityScan.vulnerabilities.slice(0, 100) as vuln (vuln.vulnerabilityId + vuln.pkgName)}
													<tr class="border-t hover:bg-muted/50">
														<td class="p-2">
															<a
																href="https://nvd.nist.gov/vuln/detail/{vuln.vulnerabilityId}"
																target="_blank"
																rel="noopener noreferrer"
																class="font-mono text-xs text-blue-600 hover:underline dark:text-blue-400"
															>
																{vuln.vulnerabilityId}
															</a>
														</td>
														<td class="p-2 font-mono text-xs">{vuln.pkgName}</td>
														<td class="p-2">
															{#if vuln.severity === 'CRITICAL'}
																<StatusBadge text={m.vuln_severity_critical()} variant="red" size="sm" />
															{:else if vuln.severity === 'HIGH'}
																<StatusBadge text={m.vuln_severity_high()} variant="orange" size="sm" />
															{:else if vuln.severity === 'MEDIUM'}
																<StatusBadge text={m.vuln_severity_medium()} variant="amber" size="sm" />
															{:else if vuln.severity === 'LOW'}
																<StatusBadge text={m.vuln_severity_low()} variant="lime" size="sm" />
															{:else}
																<StatusBadge text={m.vuln_severity_unknown()} variant="gray" size="sm" />
															{/if}
														</td>
														<td class="p-2 font-mono text-xs">{vuln.installedVersion}</td>
														<td class="p-2 font-mono text-xs">{vuln.fixedVersion || m.vuln_no_fix()}</td>
													</tr>
												{/each}
											</tbody>
										</table>
									</div>
									{#if vulnerabilityScan.vulnerabilities.length > 100}
										<p class="text-xs text-muted-foreground">
											{m.vuln_showing_first({ count: 100, total: vulnerabilityScan.vulnerabilities.length })}
										</p>
									{/if}
								</div>
							{/if}
						{/if}
					{:else if vulnerabilityScan?.status === 'failed'}
						<div class="flex items-center gap-3 rounded-lg bg-red-50 p-4 dark:bg-red-950/20">
							<ShieldAlertIcon class="size-8 text-red-500" />
							<div>
								<p class="font-medium text-red-800 dark:text-red-200">{m.vuln_scan_failed()}</p>
								<p class="text-sm text-red-600 dark:text-red-400">{vulnerabilityScan.error || m.vuln_scanner_not_installed()}</p>
							</div>
						</div>
					{:else}
						<div class="flex flex-col items-center justify-center py-8 text-center">
							<ScanIcon class="mb-4 size-12 text-muted-foreground/50" />
							<p class="mb-2 text-muted-foreground">{m.vuln_no_scan()}</p>
							<ArcaneButton action="base" onclick={handleScanImage} disabled={isLoading.scanning}>
								{#if isLoading.scanning}
									<Spinner class="mr-2 size-4" />
								{:else}
									<ScanIcon class="mr-2 size-4" />
								{/if}
								{m.vuln_scan_image()}
							</ArcaneButton>
						</div>
					{/if}
				</Card.Content>
			</Card.Root>

			{#if image.repoTags && image.repoTags.length > 0}
				<Card.Root>
					<Card.Header icon={TagIcon}>
						<div class="flex flex-col space-y-1.5">
							<Card.Title>{m.common_tags()}</Card.Title>
							<Card.Description>{m.images_tags_description()}</Card.Description>
						</div>
					</Card.Header>
					<Card.Content class="p-4">
						<div class="flex flex-wrap gap-2">
							{#each image.repoTags as tag (tag)}
								<Badge variant="secondary" class="cursor-pointer text-sm select-all" title="Click to select">{tag}</Badge>
							{/each}
						</div>
					</Card.Content>
				</Card.Root>
			{/if}

			{#if image.config?.env && image.config.env.length > 0}
				<Card.Root>
					<Card.Header icon={SettingsIcon}>
						<div class="flex flex-col space-y-1.5">
							<Card.Title>{m.common_environment_variables()}</Card.Title>
							<Card.Description>{m.images_env_vars_description()}</Card.Description>
						</div>
					</Card.Header>
					<Card.Content class="p-4">
						<div class="grid grid-cols-1 gap-3 lg:grid-cols-2 2xl:grid-cols-3">
							{#each image.config.env as env (env)}
								{#if env.includes('=')}
									{@const [key, ...valueParts] = env.split('=')}
									{@const value = valueParts.join('=')}
									<Card.Root variant="subtle">
										<Card.Content class="flex flex-col gap-2 p-4">
											<div class="text-muted-foreground text-xs font-semibold tracking-wide break-all uppercase">{key}</div>
											<div
												class="text-foreground cursor-pointer font-mono text-sm font-medium break-all select-all"
												title="Click to select"
											>
												{value}
											</div>
										</Card.Content>
									</Card.Root>
								{:else}
									<Card.Root variant="subtle">
										<Card.Content class="flex flex-col gap-2 p-4">
											<div class="text-muted-foreground text-xs font-semibold tracking-wide uppercase">ENV_VAR</div>
											<div
												class="text-foreground cursor-pointer font-mono text-sm font-medium break-all select-all"
												title="Click to select"
											>
												{env}
											</div>
										</Card.Content>
									</Card.Root>
								{/if}
							{/each}
						</div>
					</Card.Content>
				</Card.Root>
			{/if}
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
