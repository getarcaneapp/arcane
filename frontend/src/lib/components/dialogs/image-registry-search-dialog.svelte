<script lang="ts">
	import * as ResponsiveDialog from '$lib/components/ui/responsive-dialog/index.js';
	import * as InputGroup from '$lib/components/ui/input-group/index.js';
	import { ArcaneButton } from '$lib/components/arcane-button/index.js';
	import { Label } from '$lib/components/ui/label/index.js';
	import { imageService } from '$lib/services/image-service';
	import { m } from '$lib/paraglide/messages';
	import type { ImageSearchResultDto } from '$lib/types/docker';
	import { SearchIcon, DownloadIcon, VerifiedCheckIcon } from '$lib/icons';
	import { toast } from 'svelte-sonner';

	type Props = {
		open: boolean;
		onOpenChange?: (open: boolean) => void;
		onPullFinished?: (success: boolean, imageName?: string, error?: string) => Promise<void> | void;
	};

	let { open = $bindable(false), onOpenChange, onPullFinished = () => {} }: Props = $props();

	let query = $state('');
	let tag = $state('latest');
	let results = $state<ImageSearchResultDto[]>([]);
	let hasSearched = $state(false);
	let isSearching = $state(false);
	let pullingImageName = $state<string | null>(null);

	const normalizedTag = $derived(tag.trim() || 'latest');
	const canSearch = $derived(query.trim().length > 0 && !isSearching);

	function resetDialog() {
		query = '';
		tag = 'latest';
		results = [];
		hasSearched = false;
	}

	function handleOpenChange(nextOpen: boolean) {
		if (!nextOpen && pullingImageName) return;
		open = nextOpen;
		onOpenChange?.(nextOpen);
		if (!nextOpen) {
			resetDialog();
		}
	}

	async function handleSearch() {
		const term = query.trim();
		if (!term) {
			toast.error(m.images_image_required());
			return;
		}
		isSearching = true;
		hasSearched = true;
		try {
			results = await imageService.searchImages(term);
		} catch (error) {
			console.error('Failed to search images:', error);
			toast.error(m.images_search_failed());
		} finally {
			isSearching = false;
		}
	}

	function buildPullRef(name: string) {
		const trimmedName = name.trim();
		if (!trimmedName) return '';
		if (trimmedName.includes(':')) return trimmedName;
		return `${trimmedName}:${normalizedTag}`;
	}

	async function pullImage(name: string) {
		const imageRef = buildPullRef(name);
		if (!imageRef || pullingImageName) return;

		pullingImageName = imageRef;
		open = false;
		onOpenChange?.(false);
		resetDialog();

		const result = await imageService.pullImageStream(imageRef);
		pullingImageName = null;
		if (!result.success) {
			const message = result.error || m.images_pull_failed();
			toast.error(message);
			await onPullFinished(false, imageRef, message);
			return;
		}

		toast.success(m.images_pull_success({ repoTag: imageRef }));
		await onPullFinished(true, imageRef);
	}
</script>

<ResponsiveDialog.Root
	{open}
	onOpenChange={handleOpenChange}
	title={m.images_search_registry()}
	description={m.images_search_registry_description()}
	contentClass="sm:max-w-[720px]"
>
	{#snippet children()}
		<div class="grid gap-4 py-4">
			<div class="grid gap-3 sm:grid-cols-[1fr_9rem_auto]">
				<div class="space-y-2">
					<Label for="image-registry-search-query">{m.images_search_query()}</Label>
					<InputGroup.Root>
						<InputGroup.Addon>
							<SearchIcon class="size-4" />
						</InputGroup.Addon>
						<InputGroup.Input
							id="image-registry-search-query"
							placeholder={m.images_search_query_placeholder()}
							bind:value={query}
							disabled={isSearching || !!pullingImageName}
							onkeydown={(event) => {
								if (event.key === 'Enter') {
									event.preventDefault();
									void handleSearch();
								}
							}}
						/>
					</InputGroup.Root>
				</div>

				<div class="space-y-2">
					<Label for="image-registry-search-tag">{m.images_tag()}</Label>
					<InputGroup.Root>
						<InputGroup.Input
							id="image-registry-search-tag"
							placeholder={m.images_tag_latest()}
							bind:value={tag}
							disabled={isSearching || !!pullingImageName}
						/>
					</InputGroup.Root>
				</div>

				<div class="flex items-end">
					<ArcaneButton
						action="inspect"
						type="button"
						customLabel={m.images_search_registry()}
						loading={isSearching}
						disabled={!canSearch}
						onclick={handleSearch}
					/>
				</div>
			</div>

			{#if results.length > 0}
				<div class="border-border max-h-[420px] overflow-auto rounded-lg border">
					{#each results as result (result.name)}
						<div class="grid gap-3 border-b p-3 last:border-b-0 sm:grid-cols-[1fr_auto] sm:items-center">
							<div class="min-w-0 space-y-1">
								<div class="flex flex-wrap items-center gap-2">
									<span class="font-mono text-sm font-medium break-all">{result.name}</span>
									{#if result.official}
										<span class="text-emerald-400" title={m.common_verified()} aria-label={m.common_verified()}>
											<VerifiedCheckIcon class="size-3.5" aria-hidden="true" />
										</span>
									{/if}
								</div>
								<p class="text-muted-foreground line-clamp-2 text-sm">
									{result.description || m.common_no_description()}
								</p>
								<p class="text-muted-foreground text-xs">{m.images_search_stars({ count: result.starCount })}</p>
							</div>
							<ArcaneButton
								action="pull"
								type="button"
								size="sm"
								icon={DownloadIcon}
								customLabel={m.images_search_pull_result()}
								loading={pullingImageName === buildPullRef(result.name)}
								disabled={!!pullingImageName}
								onclick={() => pullImage(result.name)}
							/>
						</div>
					{/each}
				</div>
			{:else if hasSearched && !isSearching}
				<div class="border-border bg-muted/30 text-muted-foreground rounded-lg border px-4 py-8 text-center text-sm">
					{m.images_search_empty()}
				</div>
			{/if}
		</div>
	{/snippet}
</ResponsiveDialog.Root>
