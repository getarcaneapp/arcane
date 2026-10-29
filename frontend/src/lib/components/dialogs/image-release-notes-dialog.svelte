<script lang="ts">
	import { createQuery } from '@tanstack/svelte-query';

	import ReleaseNotes from '#lib/components/release-notes.svelte';
	import { ResponsiveDialog } from '#lib/components/ui/responsive-dialog/index.js';
	import { Spinner } from '#lib/components/ui/spinner/index.js';
	import { ExternalLinkIcon } from '#lib/icons/index.js';
	import { m } from '#lib/paraglide/messages.js';
	import { queryKeys } from '#lib/query/query-keys.js';
	import { imageService } from '#lib/services/image-service.js';
	import type { ImageReleaseNotesDto } from '#lib/types/docker.js';
	import { formatRelativeTime } from '#lib/utils/formatting.js';

	interface Props {
		open?: boolean;
		environmentId: string;
		imageRef: string;
		version?: string;
		digest?: string;
	}

	let { open = $bindable(false), environmentId, imageRef, version, digest }: Props = $props();

	const releaseNotesQuery = createQuery<ImageReleaseNotesDto>(() => ({
		queryKey: queryKeys.images.releaseNotes(environmentId, imageRef, version ?? '', digest ?? ''),
		queryFn: () => imageService.getImageReleaseNotes(environmentId, imageRef, { version, digest }),
		enabled: open && !!environmentId && !!imageRef,
		retry: false,
		staleTime: 10 * 60_000
	}));

	const notes = $derived(releaseNotesQuery.data);
	const targetVersion = $derived(notes?.version || version || '');
	const heading = $derived(notes?.status === 'found' ? notes.releaseName || targetVersion : '');
	const publishedLabel = $derived(formatRelativeTime(notes?.publishedAt));
	const linkHref = $derived(notes && notes.status !== 'no_source' ? notes.releaseUrl || notes.sourceUrl : undefined);
	const linkLabel = $derived(
		notes?.status !== 'link' && notes?.releaseUrl ? m.update_center_view_full_release() : m.image_release_notes_open_project()
	);
	const errorMessage = $derived(releaseNotesQuery.error instanceof Error ? releaseNotesQuery.error.message : '');
</script>

<ResponsiveDialog bind:open title={m.image_release_notes_title({ image: imageRef })} contentClass="sm:max-w-2xl">
	<div class="space-y-4 pb-6">
		{#if notes && (heading || publishedLabel || linkHref)}
			<div class="flex flex-wrap items-center justify-between gap-2">
				<div class="flex min-w-0 items-baseline gap-2">
					{#if heading}
						<span class="truncate text-sm font-semibold text-foreground">{heading}</span>
					{/if}
					{#if publishedLabel}
						<span class="shrink-0 text-xs text-muted-foreground">{publishedLabel}</span>
					{/if}
				</div>
				{#if linkHref}
					<a
						href={linkHref}
						target="_blank"
						rel="noopener noreferrer"
						class="inline-flex items-center gap-1 text-xs text-muted-foreground transition-colors hover:text-foreground"
					>
						{linkLabel}
						<ExternalLinkIcon class="size-3" />
					</a>
				{/if}
			</div>
		{/if}

		{#if releaseNotesQuery.isPending}
			<div class="flex justify-center py-6">
				<Spinner class="size-5" />
			</div>
		{:else if releaseNotesQuery.isError}
			<div class="rounded-lg border border-destructive/25 bg-destructive/10 px-3 py-2">
				<p class="text-sm font-medium text-destructive">{m.release_notes_load_failed()}</p>
				{#if errorMessage}
					<p class="mt-1 text-xs wrap-break-word text-destructive">{errorMessage}</p>
				{/if}
			</div>
		{:else if notes?.status === 'found'}
			<ReleaseNotes markdown={notes.notes ?? ''} />
		{:else if notes?.status === 'not_found'}
			<p class="text-sm text-muted-foreground">
				{targetVersion ? m.image_release_notes_not_found({ version: targetVersion }) : m.image_release_notes_unknown_version()}
			</p>
		{:else if notes?.status === 'unavailable'}
			<p class="text-sm text-muted-foreground">{m.release_notes_load_failed()}</p>
		{:else if notes?.status === 'link'}
			<p class="text-sm text-muted-foreground">{m.image_release_notes_external()}</p>
		{:else if notes?.status === 'no_source'}
			<p class="text-sm text-muted-foreground">{m.image_release_notes_no_source()}</p>
		{/if}
	</div>
</ResponsiveDialog>
