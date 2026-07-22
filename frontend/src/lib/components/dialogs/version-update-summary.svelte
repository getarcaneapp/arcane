<script lang="ts">
	import { m } from '$lib/paraglide/messages';
	import type { AppVersionInformation } from '$lib/types/settings';

	let {
		versionInformation,
		newestVersion,
		releasedAgo = ''
	}: {
		versionInformation?: AppVersionInformation;
		newestVersion?: string;
		releasedAgo?: string;
	} = $props();

	const isSemver = $derived(!!versionInformation?.isSemverVersion);
	const trackingTag = $derived(versionInformation?.currentTag ?? '');
	const currentDigest = $derived(versionInformation?.currentDigest ?? '');
	const newDigest = $derived(versionInformation?.newestDigest ?? '');
	const semverCurrent = $derived(versionInformation?.displayVersion || versionInformation?.currentVersion || '');
	const semverNew = $derived(newestVersion || versionInformation?.newestVersion || '');
</script>

{#if isSemver && (semverCurrent || semverNew)}
	<div class="flex flex-wrap items-center gap-2 text-sm">
		{#if semverCurrent}
			<span class="inline-flex items-center rounded-md bg-muted px-2 py-0.5 font-mono text-xs text-muted-foreground">
				{semverCurrent}
			</span>
		{/if}
		{#if semverCurrent && semverNew}
			<span class="text-muted-foreground/60">→</span>
		{/if}
		{#if semverNew}
			<span class="inline-flex items-center rounded-md bg-primary/10 px-2 py-0.5 font-mono text-xs font-medium text-primary">
				{semverNew}
			</span>
		{/if}
		{#if releasedAgo}
			<span class="text-xs text-muted-foreground/70">· {m.update_center_released_at({ date: releasedAgo })}</span>
		{/if}
	</div>
{:else if !isSemver && (trackingTag || currentDigest || newDigest)}
	<div class="space-y-1.5 text-xs">
		{#if trackingTag}
			<div class="flex items-baseline gap-2">
				<span class="w-16 shrink-0 tracking-wide text-muted-foreground/70 uppercase">{m.tag()}</span>
				<span class="inline-flex items-center rounded-md bg-muted px-2 py-0.5 font-mono text-foreground">
					{trackingTag}
				</span>
			</div>
		{/if}
		{#if currentDigest}
			<div class="flex items-baseline gap-2">
				<span class="w-16 shrink-0 tracking-wide text-muted-foreground/70 uppercase">{m.common_current()}</span>
				<code class="min-w-0 flex-1 rounded-md bg-muted/50 px-2 py-1 font-mono text-[11px] break-all text-muted-foreground">
					{currentDigest}
				</code>
			</div>
		{/if}
		{#if newDigest}
			<div class="flex items-baseline gap-2">
				<span class="w-16 shrink-0 tracking-wide text-primary/80 uppercase">{m.update_center_new_label()}</span>
				<code class="min-w-0 flex-1 rounded-md bg-primary/10 px-2 py-1 font-mono text-[11px] font-medium break-all text-primary">
					{newDigest}
				</code>
			</div>
		{/if}
		{#if releasedAgo}
			<p class="pt-1 text-muted-foreground/70">{m.update_center_released_at({ date: releasedAgo })}</p>
		{/if}
	</div>
{/if}
