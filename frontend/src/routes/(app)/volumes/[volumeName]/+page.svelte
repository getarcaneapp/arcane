<script lang="ts">
	// fallow-ignore-file code-duplication -- useUrlTab initialization is the hook's intended per-page integration surface
	import * as Card from '$lib/components/ui/card/index.js';
	import { VolumesIcon, ClockIcon, TagIcon, LayersIcon, InfoIcon, GlobeIcon, ContainersIcon, BoxIcon } from '$lib/icons';
	import { goto } from '$app/navigation';
	import { Badge } from '$lib/components/ui/badge';
	import { formatDateTimeShort, truncateString } from '$lib/utils/formatting';
	import { openConfirmDialog } from '$lib/components/confirm-dialog/';
	import { toast } from 'svelte-sonner';
	import { tryCatch } from '$lib/utils/api';
	import { handleApiResultWithCallbacks } from '$lib/utils/api';
	import { ArcaneButton } from '$lib/components/arcane-button/index.js';
	import { m } from '$lib/paraglide/messages';
	import { untrack } from 'svelte';
	import { volumeService } from '$lib/services/volume-service.js';
	import { ResourceDetailLayout, type DetailAction } from '$lib/layouts';
	import TabbedPageLayout from '$lib/layouts/tabbed-page-layout.svelte';
	import { VolumeBrowser } from '$lib/components/file-browser';
	import BackupList from '../components/volume-backup-table.svelte';
	import settingsStore from '$lib/stores/config-store';
	import { environmentStore } from '$lib/stores/environment.store.svelte';
	import { hasPermission } from '$lib/utils/auth';
	import { activityToastOptions, extractActivityId } from '$lib/utils/activity-toast';
	import PropertyItem from '$lib/components/property-item.svelte';
	import KeyValueGridCard from '$lib/components/key-value-grid-card.svelte';
	import InUseStatus from '$lib/components/arcane-table/cells/in-use-status.svelte';
	import { useUrlTab } from '$lib/hooks/use-url-tab.svelte';

	let { data } = $props();
	let volume = $state(untrack(() => data.volume));
	let containersDetailed = $state<{ id: string; name: string }[]>(untrack(() => data.containersDetailed ?? []));

	const backupVolumeName = $derived.by(() => $settingsStore?.backupVolumeName || 'arcane-backups');
	const isBackupVolume = $derived(volume?.name === backupVolumeName);

	const currentEnvId = $derived(environmentStore.selected?.id || '0');
	const canDeleteVolume = $derived(hasPermission('volumes:delete', currentEnvId));

	let isLoading = $state({ remove: false });
	const createdDate = $derived(volume.createdAt ? formatDateTimeShort(volume.createdAt) : m.common_unknown());

	const tabItems = $derived([
		{ value: 'overview', label: m.common_overview() },
		{ value: 'browser', label: m.volumes_nav_browser() },
		{ value: 'backups', label: m.volumes_nav_backups() }
	]);
	const urlTab = useUrlTab({
		validTabs: () => tabItems.map((tab) => tab.value),
		defaultTab: () => 'overview'
	});
	const selectedTab = $derived(urlTab.value);

	async function handleRemoveVolumeConfirm(volumeName: string) {
		const safeName = volumeName?.trim() || m.common_unknown();
		if (safeName === backupVolumeName) return;
		const message = volume.inUse
			? `${m.volumes_remove_confirm_message({ name: safeName })}\n\n${m.volumes_remove_in_use_warning()}`
			: m.volumes_remove_confirm_message({ name: safeName });

		openConfirmDialog({
			title: m.common_remove_title({ resource: m.resource_volume() }),
			message,
			confirm: {
				label: m.common_remove(),
				destructive: true,
				action: async () => {
					handleApiResultWithCallbacks({
						result: await tryCatch(volumeService.deleteVolume(safeName)),
						message: m.volumes_remove_failed({ name: safeName }),
						setLoadingState: (value) => (isLoading.remove = value),
						onSuccess: async (data) => {
							toast.success(m.volumes_remove_success({ name: safeName }), activityToastOptions(extractActivityId(data)));
							goto('/volumes');
						}
					});
				}
			}
		});
	}

	const actions: DetailAction[] = $derived(
		canDeleteVolume
			? [
					{
						id: 'remove',
						action: 'remove' as const,
						label: m.common_remove(),
						loading: isLoading.remove,
						disabled: isLoading.remove || isBackupVolume,
						onclick: () => handleRemoveVolumeConfirm(volume.name)
					}
				]
			: []
	);

	function onTabChange(value: string) {
		urlTab.select(value);
	}
</script>

{#if volume}
	<TabbedPageLayout backUrl="/volumes" backLabel={m.resource_volumes_cap()} {tabItems} {selectedTab} {onTabChange}>
		{#snippet headerInfo()}
			<div class="flex flex-col gap-1">
				<h1 class="text-2xl font-semibold tracking-tight break-all sm:text-3xl">{volume.name}</h1>
				<div class="flex flex-wrap items-center gap-2 pt-1">
					<InUseStatus inUse={volume.inUse} />
					{#if volume.driver}
						<Badge variant="blue" minWidth="20">{volume.driver}</Badge>
					{/if}
					{#if volume.scope}
						<Badge variant="purple" minWidth="20">{volume.scope}</Badge>
					{/if}
				</div>
			</div>
		{/snippet}

		{#snippet headerActions()}
			<div class="flex items-center gap-2">
				{#each actions as act (act.id)}
					<ArcaneButton
						action={act.action}
						customLabel={act.label}
						loading={act.loading}
						disabled={act.disabled}
						onclick={act.onclick}
					/>
				{/each}
			</div>
		{/snippet}

		{#snippet tabContent(tab)}
			<div class="space-y-6">
				{#if tab === 'overview'}
					<Card.Root>
						<Card.Header icon={InfoIcon}>
							<div class="flex flex-col space-y-1.5">
								<Card.Title>{m.common_details_title({ resource: m.resource_volume_cap() })}</Card.Title>
								<Card.Description>{m.common_details_description({ resource: m.resource_volume() })}</Card.Description>
							</div>
						</Card.Header>
						<Card.Content class="p-4">
							<div class="grid grid-cols-1 gap-x-4 gap-y-6 sm:grid-cols-2 lg:grid-cols-3 xl:grid-cols-4 2xl:grid-cols-6">
								<PropertyItem
									icon={BoxIcon}
									color="gray"
									label={m.common_name()}
									value={volume.name}
									valueClass="mt-1 cursor-pointer text-sm font-semibold break-all select-all sm:text-base"
								/>

								<PropertyItem icon={VolumesIcon} color="blue" label={m.common_driver()} value={volume.driver} />

								<PropertyItem icon={ClockIcon} color="green" label={m.common_created()} value={createdDate} />

								<PropertyItem
									icon={GlobeIcon}
									color="purple"
									label={m.common_scope()}
									value={volume.scope}
									valueClass="mt-1 cursor-pointer text-sm font-semibold capitalize select-all sm:text-base"
								/>

								<PropertyItem icon={InfoIcon} color="amber" label={m.common_status()}>
									<p class="mt-1 text-base font-semibold">
										<InUseStatus inUse={volume.inUse} />
									</p>
								</PropertyItem>

								<PropertyItem
									icon={LayersIcon}
									color="teal"
									label={m.common_mountpoint()}
									class="col-span-1 flex items-start gap-3 sm:col-span-2 lg:col-span-3 xl:col-span-4 2xl:col-span-6"
								>
									<div
										class="mt-2 cursor-pointer rounded-lg border bg-muted/50 p-3 select-all"
										title={m.common_click_to_select()}
									>
										<code class="font-mono text-sm break-all">{volume.mountpoint}</code>
									</div>
								</PropertyItem>
							</div>
						</Card.Content>
					</Card.Root>

					<Card.Root>
						<Card.Header icon={ContainersIcon}>
							<div class="flex flex-col space-y-1.5">
								<Card.Title>{m.volumes_containers_using_title()}</Card.Title>
								<Card.Description>{m.volumes_containers_using_description()}</Card.Description>
							</div>
						</Card.Header>
						<Card.Content class="p-4">
							{#if containersDetailed.length > 0}
								<Card.Root variant="outlined">
									<Card.Content class="divide-y p-0">
										{#each containersDetailed as c (c.id)}
											<div class="flex flex-col p-3 sm:flex-row sm:items-center">
												<div class="mb-2 w-full font-medium break-all sm:mb-0 sm:w-1/3">
													<a href="/containers/{c.id}" class="flex items-center text-primary hover:underline">
														<ContainersIcon class="mr-1.5 size-3.5 text-muted-foreground" />
														{c.name}
													</a>
												</div>
												<div class="w-full pl-0 sm:w-2/3 sm:pl-4">
													<code
														class="cursor-pointer rounded bg-muted px-1.5 py-0.5 font-mono text-xs break-all text-muted-foreground select-all sm:text-sm"
														title={m.common_click_to_select()}
													>
														{truncateString(c.id, 48)}
													</code>
												</div>
											</div>
										{/each}
									</Card.Content>
								</Card.Root>
							{:else if volume.containers && volume.containers.length > 0}
								<!-- Fallback to IDs if names not resolved -->
								<Card.Root variant="subtle">
									<Card.Content class="divide-y p-0">
										{#each volume.containers as id (id)}
											<div class="flex items-center justify-between gap-3 p-3">
												<code class="font-mono text-sm break-all">{truncateString(id, 48)}</code>
												<a href={`/containers/${id}`} class="text-sm text-primary hover:underline">{m.common_view()}</a>
											</div>
										{/each}
									</Card.Content>
								</Card.Root>
							{:else}
								<div class="text-muted-foreground">{m.volumes_no_containers_using()}</div>
							{/if}
						</Card.Content>
					</Card.Root>

					{#if volume.labels && Object.keys(volume.labels).length > 0}
						<KeyValueGridCard
							icon={TagIcon}
							title={m.common_labels()}
							description={m.volumes_labels_description()}
							entries={Object.entries(volume.labels)}
						/>
					{/if}

					{#if volume.options && Object.keys(volume.options).length > 0}
						<KeyValueGridCard
							icon={VolumesIcon}
							title={m.common_driver_options()}
							description={m.volumes_driver_options_description()}
							entries={Object.entries(volume.options)}
						/>
					{/if}

					{#if (!volume.labels || Object.keys(volume.labels).length === 0) && (!volume.options || Object.keys(volume.options).length === 0)}
						<Card.Root class="border bg-muted/10 shadow-sm">
							<Card.Content class="pt-6 pb-6 text-center">
								<div class="flex flex-col items-center justify-center">
									<div class="mb-4 rounded-full bg-muted/30 p-3">
										<TagIcon class="size-5 text-muted-foreground opacity-50" />
									</div>
									<p class="text-muted-foreground">{m.volumes_no_labels_or_options()}</p>
								</div>
							</Card.Content>
						</Card.Root>
					{/if}
				{:else if tab === 'browser'}
					<VolumeBrowser volumeName={volume.name} />
				{:else if tab === 'backups'}
					<BackupList volumeName={volume.name} />
				{/if}
			</div>
		{/snippet}
	</TabbedPageLayout>
{:else}
	<ResourceDetailLayout backUrl="/volumes" backLabel={m.resource_volumes_cap()} title={m.resource_volume_cap()} {actions}>
		<div class="flex flex-col items-center justify-center px-4 py-16 text-center">
			<div class="mb-4 rounded-full bg-muted/30 p-4">
				<BoxIcon class="size-10 text-muted-foreground opacity-70" />
			</div>
			<h2 class="mb-2 text-xl font-medium">{m.common_not_found_title({ resource: m.resource_volumes_cap() })}</h2>
			<p class="mb-6 text-muted-foreground">
				{m.common_not_found_description({ resource: m.resource_volumes_cap().toLowerCase() })}
			</p>

			<ArcaneButton
				action="cancel"
				customLabel={m.common_back_to({ resource: m.resource_volumes_cap() })}
				onclick={() => goto('/volumes')}
				size="sm"
			/>
		</div>
	</ResourceDetailLayout>
{/if}
