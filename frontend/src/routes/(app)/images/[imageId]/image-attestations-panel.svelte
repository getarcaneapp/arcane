<script lang="ts">
	import { ResponsiveDialog } from '$lib/components/ui/responsive-dialog/index.js';
	import * as Select from '$lib/components/ui/select/index.js';
	import { Badge } from '$lib/components/ui/badge';
	import { Spinner } from '$lib/components/ui/spinner';
	import { ArrowRightIcon } from '$lib/icons';
	import { m } from '$lib/paraglide/messages';
	import { imageService } from '$lib/services/image-service';
	import { environmentStore } from '$lib/stores/environment.store.svelte';
	import type { ImageAttestationDto, ImageAttestationListDto, ImageDetailSummaryDto } from '$lib/types/docker';
	import { bytes } from '$lib/utils/formatting';

	const ALL_FILTER = '__all__';

	let { image }: { image: ImageDetailSummaryDto } = $props();

	const envId = $derived(environmentStore.selected?.id || '0');
	let selectedPlatform = $state(ALL_FILTER);
	let selectedPredicateType = $state(ALL_FILTER);
	let detailsOpen = $state(false);
	let selectedAttestation = $state<ImageAttestationDto | null>(null);

	function openDetails(attestation: ImageAttestationDto) {
		selectedAttestation = attestation;
		detailsOpen = true;
	}

	const requestOptions = $derived.by(() => ({
		platform: selectedPlatform === ALL_FILTER ? undefined : selectedPlatform,
		predicateType: selectedPredicateType === ALL_FILTER ? undefined : selectedPredicateType
	}));

	const attestationsPromise = $derived.by(() => {
		if (!image.id || !envId) {
			return Promise.resolve(emptyAttestationList());
		}
		return imageService.getImageAttestationsForEnvironment(envId, image.id, requestOptions);
	});
	const hasActiveFilter = $derived(selectedPlatform !== ALL_FILTER || selectedPredicateType !== ALL_FILTER);
	const selectedPlatformLabel = $derived(
		selectedPlatform === ALL_FILTER ? m.images_attestations_all_platforms() : selectedPlatform
	);
	const selectedPredicateLabel = $derived(
		selectedPredicateType === ALL_FILTER ? m.images_attestations_all_predicates() : selectedPredicateType
	);

	function filterOptions(values: Array<string | undefined>, selected: string): string[] {
		const options = values.filter((value): value is string => Boolean(value));
		if (selected !== ALL_FILTER) {
			options.push(selected);
		}
		return options.filter((value, index) => options.indexOf(value) === index).sort();
	}

	function subjectLabel(attestation: ImageAttestationDto): string {
		if (!attestation.subject.length) return m.common_na();
		return attestation.subject.map((subject) => subject.name || Object.values(subject.digest).join(', ')).join(', ');
	}

	function emptyAttestationList(): ImageAttestationListDto {
		return {
			imageRef: image.id,
			subjectDigest: '',
			attestations: []
		};
	}
</script>

<div class="space-y-4">
	{#await attestationsPromise}
		<div class="flex items-center justify-center py-8">
			<Spinner class="size-7" />
			<span class="text-muted-foreground ml-3 text-sm">{m.images_attestations_loading()}</span>
		</div>
	{:then data}
		{@const attestations = data.attestations}
		{@const knownPlatforms = filterOptions(
			attestations.map((attestation) => attestation.platform),
			selectedPlatform
		)}
		{@const knownPredicateTypes = filterOptions(
			attestations.map((attestation) => attestation.predicateType),
			selectedPredicateType
		)}
		<div class="flex flex-wrap items-center gap-3">
			<Select.Root type="single" bind:value={selectedPlatform}>
				<Select.Trigger size="sm" class="w-full sm:w-[180px]">
					<span class="truncate">{selectedPlatformLabel}</span>
				</Select.Trigger>
				<Select.Content>
					<Select.Item value={ALL_FILTER}>{m.images_attestations_all_platforms()}</Select.Item>
					{#each knownPlatforms as platform (platform)}
						<Select.Item value={platform}>{platform}</Select.Item>
					{/each}
				</Select.Content>
			</Select.Root>

			<Select.Root type="single" bind:value={selectedPredicateType}>
				<Select.Trigger size="sm" class="w-full sm:w-[280px]">
					<span class="truncate">{selectedPredicateLabel}</span>
				</Select.Trigger>
				<Select.Content>
					<Select.Item value={ALL_FILTER}>{m.images_attestations_all_predicates()}</Select.Item>
					{#each knownPredicateTypes as predicateType (predicateType)}
						<Select.Item value={predicateType}>
							<span class="font-mono text-xs">{predicateType}</span>
						</Select.Item>
					{/each}
				</Select.Content>
			</Select.Root>
		</div>

		{#if attestations.length === 0}
			<div class="text-muted-foreground bg-muted/40 rounded-lg p-4 text-sm">
				{#if hasActiveFilter}
					{m.images_attestations_no_matches()}
				{:else}
					{m.images_attestations_none_found()}
				{/if}
			</div>
		{:else}
			<div class="border-border/60 divide-border/60 divide-y overflow-hidden rounded-lg border">
				{#each attestations as attestation (attestation.digest + attestation.predicateType + (attestation.platform ?? ''))}
					<button
						type="button"
						onclick={() => openDetails(attestation)}
						class="hover:bg-muted/40 flex w-full items-center gap-3 px-4 py-3 text-left transition-colors"
					>
						<span class="min-w-0 flex-1 truncate font-mono text-xs font-semibold">{attestation.predicateType}</span>
						{#if attestation.platform}
							<Badge variant="outline" class="shrink-0">{attestation.platform}</Badge>
						{/if}
						<ArrowRightIcon class="text-muted-foreground size-4 shrink-0" />
					</button>
				{/each}
			</div>
		{/if}
	{:catch}
		<div class="bg-muted/40 rounded-lg p-4">
			<p class="text-sm font-medium">{m.images_attestations_lookup_failed()}</p>
			<p class="text-muted-foreground mt-1 text-xs">{m.images_attestations_lookup_failed_description()}</p>
		</div>
	{/await}

	<ResponsiveDialog bind:open={detailsOpen} title={m.images_attestations_details_title()} contentClass="sm:max-w-2xl">
		{#if selectedAttestation}
			{@const attestation = selectedAttestation}
			<div class="space-y-4 pb-6">
				<div class="grid gap-3 sm:grid-cols-2">
					<div>
						<div class="text-muted-foreground text-xs font-semibold tracking-wide uppercase">
							{m.images_attestations_predicate_type()}
						</div>
						<p class="mt-1 font-mono text-xs break-all">{attestation.predicateType}</p>
					</div>
					<div>
						<div class="text-muted-foreground text-xs font-semibold tracking-wide uppercase">
							{m.images_attestations_platform()}
						</div>
						<p class="mt-1 text-xs">{attestation.platform || m.common_na()}</p>
					</div>
					<div>
						<div class="text-muted-foreground text-xs font-semibold tracking-wide uppercase">
							{m.images_attestations_statement_type()}
						</div>
						<p class="mt-1 font-mono text-xs break-all">{attestation.statementType || m.common_na()}</p>
					</div>
					<div>
						<div class="text-muted-foreground text-xs font-semibold tracking-wide uppercase">
							{m.images_attestations_media_type()}
						</div>
						<p class="mt-1 font-mono text-xs break-all">{attestation.mediaType}</p>
					</div>
					<div>
						<div class="text-muted-foreground text-xs font-semibold tracking-wide uppercase">{m.common_size()}</div>
						<p class="mt-1 text-xs font-medium">{bytes.format(attestation.size)}</p>
					</div>
					<div class="sm:col-span-2">
						<div class="text-muted-foreground text-xs font-semibold tracking-wide uppercase">
							{m.images_attestations_digest()}
						</div>
						<p class="mt-1 font-mono text-xs break-all select-all">{attestation.digest}</p>
					</div>
					<div class="sm:col-span-2">
						<div class="text-muted-foreground text-xs font-semibold tracking-wide uppercase">
							{m.images_attestations_subject()}
						</div>
						<p class="mt-1 font-mono text-xs break-all">{subjectLabel(attestation)}</p>
					</div>
				</div>
			</div>
		{/if}
	</ResponsiveDialog>
</div>
