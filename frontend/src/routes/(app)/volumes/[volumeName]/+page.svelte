<script lang="ts">
	import * as Card from '$lib/components/ui/card/index.js';
	import { VolumesIcon, ClockIcon, TagIcon, LayersIcon, InfoIcon, GlobeIcon, ContainersIcon, BoxIcon } from '$lib/icons';
	import { goto } from '$app/navigation';
	import StatusBadge from '$lib/components/badges/status-badge.svelte';
	import { truncateString } from '$lib/utils/string.utils';
	import { openConfirmDialog } from '$lib/components/confirm-dialog/';
	import { toast } from 'svelte-sonner';
	import { tryCatch } from '$lib/utils/try-catch';
	import { handleApiResultWithCallbacks } from '$lib/utils/api.util';
	import { ArcaneButton } from '$lib/components/arcane-button/index.js';
	import { format } from 'date-fns';
	import { m } from '$lib/paraglide/messages';
	import { untrack } from 'svelte';
	import { volumeService } from '$lib/services/volume-service.js';
	import { ResourceDetailLayout, type DetailAction } from '$lib/layouts';

	let { data } = $props();
	let volume = $state(untrack(() => data.volume));
	let containersDetailed = $state<{ id: string; name: string }[]>(untrack(() => data.containersDetailed ?? []));

	let isLoading = $state({ remove: false });
	const createdDate = $derived(volume.createdAt ? format(new Date(volume.createdAt), 'PP p') : m.common_unknown());

	async function handleRemoveVolumeConfirm(volumeName: string) {
		const safeName = volumeName?.trim() || m.common_unknown();
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
						onSuccess: async () => {
							toast.success(m.volumes_remove_success({ name: safeName }));
							goto('/volumes');
						}
					});
				}
			}
		});
	}

	const actions: DetailAction[] = $derived([
		{
			id: 'remove',
			action: 'remove',
			label: m.common_remove(),
			loading: isLoading.remove,
			disabled: isLoading.remove,
			onclick: () => handleRemoveVolumeConfirm(volume.name)
		}
	]);
</script>

<ResourceDetailLayout backUrl="/volumes" backLabel={m.volumes_title()} title={volume.name} {actions}>
	{#snippet badges()}
		{#if volume.inUse}
			<StatusBadge variant="green" text={m.common_in_use()} />
		{:else}
			<StatusBadge variant="amber" text={m.common_unused()} />
		{/if}
		{#if volume.driver}
			<StatusBadge variant="blue" text={volume.driver} />
		{/if}
		{#if volume.scope}
			<StatusBadge variant="purple" text={volume.scope} />
		{/if}
	{/snippet}

	{#if volume}
		<div class="space-y-6">
			<Card.Root>
				<Card.Header icon={InfoIcon}>
					<div class="flex flex-col space-y-1.5">
						<Card.Title>{m.common_details_title({ resource: m.resource_volume_cap() })}</Card.Title>
						<Card.Description>{m.common_details_description({ resource: m.resource_volume() })}</Card.Description>
					</div>
				</Card.Header>
				<Card.Content class="p-4">
					<div class="grid grid-cols-1 gap-x-4 gap-y-6 sm:grid-cols-2 lg:grid-cols-3 xl:grid-cols-4 2xl:grid-cols-6">
						<div class="flex items-start gap-3">
							<div class="flex size-10 shrink-0 items-center justify-center rounded-full bg-gray-500/10 p-2">
								<BoxIcon class="size-5 text-gray-500" />
							</div>
							<div class="min-w-0 flex-1">
								<p class="text-muted-foreground text-sm font-medium">{m.common_name()}</p>
								<p class="mt-1 cursor-pointer text-sm font-semibold break-all select-all sm:text-base" title="Click to select">
									{volume.name}
								</p>
							</div>
						</div>

						<div class="flex items-start gap-3">
							<div class="flex size-10 shrink-0 items-center justify-center rounded-full bg-blue-500/10 p-2">
								<VolumesIcon class="size-5 text-blue-500" />
							</div>
							<div class="min-w-0 flex-1">
								<p class="text-muted-foreground text-sm font-medium">{m.common_driver()}</p>
								<p class="mt-1 cursor-pointer text-sm font-semibold select-all sm:text-base" title="Click to select">
									{volume.driver}
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
									{createdDate}
								</p>
							</div>
						</div>

						<div class="flex items-start gap-3">
							<div class="flex size-10 shrink-0 items-center justify-center rounded-full bg-purple-500/10 p-2">
								<GlobeIcon class="size-5 text-purple-500" />
							</div>
							<div class="min-w-0 flex-1">
								<p class="text-muted-foreground text-sm font-medium">{m.common_scope()}</p>
								<p class="mt-1 cursor-pointer text-sm font-semibold capitalize select-all sm:text-base" title="Click to select">
									{volume.scope}
								</p>
							</div>
						</div>

						<div class="flex items-start gap-3">
							<div class="flex size-10 shrink-0 items-center justify-center rounded-full bg-amber-500/10 p-2">
								<InfoIcon class="size-5 text-amber-500" />
							</div>
							<div class="min-w-0 flex-1">
								<p class="text-muted-foreground text-sm font-medium">{m.common_status()}</p>
								<p class="mt-1 text-base font-semibold">
									{#if volume.inUse}
										<StatusBadge variant="green" text={m.common_in_use()} />
									{:else}
										<StatusBadge variant="amber" text={m.common_unused()} />
									{/if}
								</p>
							</div>
						</div>

						<div class="col-span-1 flex items-start gap-3 sm:col-span-2 lg:col-span-3 xl:col-span-4 2xl:col-span-6">
							<div class="flex size-10 shrink-0 items-center justify-center rounded-full bg-teal-500/10 p-2">
								<LayersIcon class="size-5 text-teal-500" />
							</div>
							<div class="min-w-0 flex-1">
								<p class="text-muted-foreground text-sm font-medium">{m.common_mountpoint()}</p>
								<div class="bg-muted/50 mt-2 cursor-pointer rounded-lg border p-3 select-all" title="Click to select">
									<code class="font-mono text-sm break-all">{volume.mountpoint}</code>
								</div>
							</div>
						</div>
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
											<a href="/containers/{c.id}" class="text-primary flex items-center hover:underline">
												<ContainersIcon class="text-muted-foreground mr-1.5 size-3.5" />
												{c.name}
											</a>
										</div>
										<div class="w-full pl-0 sm:w-2/3 sm:pl-4">
											<code
												class="bg-muted text-muted-foreground cursor-pointer rounded px-1.5 py-0.5 font-mono text-xs break-all select-all sm:text-sm"
												title="Click to select"
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
										<a href={`/containers/${id}`} class="text-primary text-sm hover:underline">{m.common_view()}</a>
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
				<Card.Root>
					<Card.Header icon={TagIcon}>
						<div class="flex flex-col space-y-1.5">
							<Card.Title>{m.common_labels()}</Card.Title>
							<Card.Description>{m.volumes_labels_description()}</Card.Description>
						</div>
					</Card.Header>
					<Card.Content class="p-4">
						<div class="grid grid-cols-1 gap-3 lg:grid-cols-2 2xl:grid-cols-3">
							{#each Object.entries(volume.labels) as [key, value] (key)}
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
							{/each}
						</div>
					</Card.Content>
				</Card.Root>
			{/if}

			{#if volume.options && Object.keys(volume.options).length > 0}
				<Card.Root>
					<Card.Header icon={VolumesIcon}>
						<div class="flex flex-col space-y-1.5">
							<Card.Title>{m.common_driver_options()}</Card.Title>
							<Card.Description>{m.volumes_driver_options_description()}</Card.Description>
						</div>
					</Card.Header>
					<Card.Content class="p-4">
						<div class="grid grid-cols-1 gap-3 lg:grid-cols-2 2xl:grid-cols-3">
							{#each Object.entries(volume.options) as [key, value] (key)}
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
							{/each}
						</div>
					</Card.Content>
				</Card.Root>
			{/if}

			{#if (!volume.labels || Object.keys(volume.labels).length === 0) && (!volume.options || Object.keys(volume.options).length === 0)}
				<Card.Root class="bg-muted/10 border shadow-sm">
					<Card.Content class="pt-6 pb-6 text-center">
						<div class="flex flex-col items-center justify-center">
							<div class="bg-muted/30 mb-4 rounded-full p-3">
								<TagIcon class="text-muted-foreground size-5 opacity-50" />
							</div>
							<p class="text-muted-foreground">{m.volumes_no_labels_or_options()}</p>
						</div>
					</Card.Content>
				</Card.Root>
			{/if}
		</div>
	{:else}
		<div class="flex flex-col items-center justify-center px-4 py-16 text-center">
			<div class="bg-muted/30 mb-4 rounded-full p-4">
				<BoxIcon class="text-muted-foreground size-10 opacity-70" />
			</div>
			<h2 class="mb-2 text-xl font-medium">{m.common_not_found_title({ resource: m.volumes_title() })}</h2>
			<p class="text-muted-foreground mb-6">{m.common_not_found_description({ resource: m.volumes_title().toLowerCase() })}</p>

			<ArcaneButton
				action="cancel"
				customLabel={m.common_back_to({ resource: m.volumes_title() })}
				onclick={() => goto('/volumes')}
				size="sm"
			/>
		</div>
	{/if}
</ResourceDetailLayout>
