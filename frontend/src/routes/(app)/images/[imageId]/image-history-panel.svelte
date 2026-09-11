<script lang="ts">
	import { createQuery } from '@tanstack/svelte-query';
	import { queryKeys } from '#lib/query/query-keys.js';
	import { environmentStore } from '#lib/stores/environment.store.svelte.js';
	import userStore from '#lib/stores/user-store.svelte.js';
	import { hasPermission } from '#lib/utils/auth.js';

	import * as Card from '#lib/components/ui/card/index.js';
	import { Badge } from '#lib/components/ui/badge/index.js';
	import { Spinner } from '#lib/components/ui/spinner/index.js';
	import { bytes, formatDateTimeShort } from '#lib/utils/formatting.js';
	import { imageService } from '#lib/services/image-service.js';
	import { m } from '#lib/paraglide/messages.js';
	import { Temporal } from 'temporal-polyfill';

	let { imageId }: { imageId: string } = $props();

	const historyQuery = createQuery(() => {
		const environmentId = environmentStore.selected?.id;
		const requestedImageId = imageId;
		userStore.current;
		return {
			queryKey: queryKeys.images.history(environmentId ?? '', requestedImageId),
			queryFn: async () => {
				await environmentStore.ready;
				return imageService.getImageHistory(requestedImageId);
			},
			enabled: !!environmentId && !!requestedImageId && hasPermission('images:read', environmentId)
		};
	});
	const history = $derived(historyQuery.data ?? []);
	const loading = $derived(historyQuery.isPending);
	const error = $derived.by(() => {
		if (historyQuery.error) return m.images_history_load_failed();
		return null;
	});

	function formatCreated(created: number) {
		if (!created) return m.common_na();
		return formatDateTimeShort(Temporal.Instant.fromEpochMilliseconds(created * 1000));
	}
</script>

<Card.Root variant="subtle">
	<Card.Header>
		<Card.Title>{m.images_history_title()}</Card.Title>
		<Card.Description>{m.images_history_description()}</Card.Description>
	</Card.Header>
	<Card.Content>
		{#if loading}
			<div class="flex items-center gap-2 py-8 text-sm text-muted-foreground">
				<Spinner class="size-4" />
				{m.images_history_loading()}
			</div>
		{:else if error}
			<p class="py-8 text-sm text-destructive">{error}</p>
		{:else if history.length === 0}
			<p class="py-8 text-sm text-muted-foreground">{m.images_history_empty()}</p>
		{:else}
			<div class="overflow-x-auto">
				<table class="w-full min-w-[720px] text-sm">
					<thead class="border-b text-left text-xs text-muted-foreground uppercase">
						<tr>
							<th class="py-2 pr-4 font-medium">{m.common_id()}</th>
							<th class="py-2 pr-4 font-medium">{m.common_created()}</th>
							<th class="py-2 pr-4 font-medium">{m.common_size()}</th>
							<th class="py-2 pr-4 font-medium">{m.common_command()}</th>
							<th class="py-2 font-medium">{m.common_tags()}</th>
						</tr>
					</thead>
					<tbody>
						{#each history as item, index (`${item.id}-${index}`)}
							<tr class="border-b last:border-0">
								<td class="max-w-[180px] py-3 pr-4 font-mono text-xs break-all">{item.id || m.images_history_missing_layer()}</td>
								<td class="py-3 pr-4 whitespace-nowrap">{formatCreated(item.created)}</td>
								<td class="py-3 pr-4 whitespace-nowrap">{bytes.format(item.size)}</td>
								<td class="max-w-[320px] py-3 pr-4 font-mono text-xs break-all">{item.createdBy || m.common_na()}</td>
								<td class="py-3">
									<div class="flex flex-wrap gap-1">
										{#each item.tags ?? [] as tag (tag)}
											<Badge variant="secondary" class="text-xs">{tag}</Badge>
										{:else}
											<span class="text-muted-foreground">{m.common_na()}</span>
										{/each}
									</div>
								</td>
							</tr>
						{/each}
					</tbody>
				</table>
			</div>
		{/if}
	</Card.Content>
</Card.Root>
