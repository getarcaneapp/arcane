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
	import { ResourceDetailLayout, type DetailAction } from '$lib/layouts';
	import StatusBadge from '$lib/components/badges/status-badge.svelte';
	import { VolumesIcon, ClockIcon, TagIcon, LayersIcon, CpuIcon, InfoIcon, SettingsIcon, HashIcon } from '$lib/icons';

	let { data } = $props();
	let { image } = $derived(data);

	let isLoading = $state({
		pulling: false,
		removing: false,
		refreshing: false
	});

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
