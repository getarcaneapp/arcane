<script lang="ts">
	import * as Card from '$lib/components/ui/card';
	import { Badge } from '$lib/components/ui/badge';
	import { Spinner } from '$lib/components/ui/spinner';
	import { bytes } from '$lib/utils/formatting';
	import { imageService } from '$lib/services/image-service';
	import type { ImageHistoryItemDto } from '$lib/types/docker';
	import { m } from '$lib/paraglide/messages';
	import { format } from 'date-fns';

	let { imageId }: { imageId: string } = $props();

	let history = $state<ImageHistoryItemDto[]>([]);
	let loading = $state(true);
	let error = $state<string | null>(null);

	$effect(() => {
		if (!imageId) return;
		void loadHistory(imageId);
	});

	async function loadHistory(id: string) {
		loading = true;
		error = null;
		try {
			history = await imageService.getImageHistory(id);
		} catch (err) {
			console.error('Failed to load image history:', err);
			error = m.images_history_load_failed();
		} finally {
			loading = false;
		}
	}

	function formatCreated(created: number) {
		if (!created) return m.common_na();
		return format(new Date(created * 1000), 'PP p');
	}
</script>

<Card.Root variant="subtle">
	<Card.Header>
		<Card.Title>{m.images_history_title()}</Card.Title>
		<Card.Description>{m.images_history_description()}</Card.Description>
	</Card.Header>
	<Card.Content>
		{#if loading}
			<div class="text-muted-foreground flex items-center gap-2 py-8 text-sm">
				<Spinner class="size-4" />
				{m.images_history_loading()}
			</div>
		{:else if error}
			<p class="text-destructive py-8 text-sm">{error}</p>
		{:else if history.length === 0}
			<p class="text-muted-foreground py-8 text-sm">{m.images_history_empty()}</p>
		{:else}
			<div class="overflow-x-auto">
				<table class="w-full min-w-[720px] text-sm">
					<thead class="text-muted-foreground border-b text-left text-xs uppercase">
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
