<script lang="ts">
	import { createQuery } from '@tanstack/svelte-query';

	import { ArcaneButton } from '#lib/components/arcane-button/index.js';
	import ReleaseNotes from '#lib/components/release-notes.svelte';
	import * as Dialog from '#lib/components/ui/dialog/index.js';
	import Spinner from '#lib/components/ui/spinner/spinner.svelte';
	import { AlertTriangleIcon, ExternalLinkIcon, SuccessIcon } from '#lib/icons/index.js';
	import { m } from '#lib/paraglide/messages.js';
	import { APIError } from '#lib/services/api-service.js';
	import systemUpgradeService from '#lib/services/api/system-upgrade-service.js';
	import { MANAGER_ENVIRONMENT_ID, isActiveUpgradeJob, upgradeJobStore } from '#lib/stores/upgrade-job.store.svelte.js';
	import type { AppVersionInformation } from '#lib/types/settings.js';
	import type { UpgradeJob } from '#lib/types/system-upgrade.js';
	import { handleApiResultWithCallbacks } from '#lib/utils/api.js';
	import { formatRelativeTime } from '#lib/utils/formatting.js';
	import { tryCatch } from '#lib/utils/try-catch.js';

	import UpgradeJobRow from './upgrade-job-row.svelte';
	import VersionUpdateSummary from './version-update-summary.svelte';

	// open/upgrading have no $bindable fallback: upstream binds can start out
	// undefined, and binding undefined to a $bindable with a fallback throws
	// props_invalid_value. Undefined reads as falsy.
	let {
		open = $bindable(undefined),
		versionInformation,
		onFinished,
		environmentName,
		environmentId,
		canInstall = true,
		debug = false,
		upgrading = $bindable(undefined)
	}: {
		open?: boolean;
		versionInformation?: AppVersionInformation;
		onFinished?: () => void | Promise<void>;
		environmentName?: string;
		environmentId?: string;
		canInstall?: boolean;
		debug?: boolean;
		upgrading?: boolean;
	} = $props();

	const isRemoteEnvironment = $derived(!!environmentName);
	const targetEnvironmentId = $derived(environmentId ?? MANAGER_ENVIRONMENT_ID);
	const isSemver = $derived(!!versionInformation?.isSemverVersion);

	const debugReleaseEnabled = $derived(open && debug && !versionInformation?.releaseNotes);
	const debugReleaseQuery = createQuery(() => ({
		queryKey: ['github', 'getarcaneapp', 'arcane', 'latest-release'],
		queryFn: async () => {
			const res = await fetch('https://api.github.com/repos/getarcaneapp/arcane/releases/latest', {
				headers: { Accept: 'application/vnd.github+json' }
			});
			if (!res.ok) throw new Error(`GitHub returned ${res.status}`);
			const data = await res.json();
			return {
				tag: (data.tag_name as string | undefined) ?? '',
				body: (data.body as string | undefined) ?? '',
				publishedAt: (data.published_at as string | undefined) ?? '',
				url: (data.html_url as string | undefined) ?? ''
			};
		},
		enabled: debugReleaseEnabled,
		staleTime: 5 * 60 * 1000,
		retry: false
	}));

	const debugRelease = $derived(debugReleaseQuery.data ?? null);
	const debugFetching = $derived(debugReleaseQuery.isFetching);
	const debugFetchSettled = $derived(debugReleaseQuery.isSuccess || debugReleaseQuery.isError);

	const trackingTag = $derived(versionInformation?.currentTag ?? '');
	const newestVersion = $derived(versionInformation?.newestVersion || debugRelease?.tag || '');

	const installLabel = $derived.by(() => {
		if (isSemver && versionInformation?.newestVersion) {
			return m.upgrade_to_version({ version: versionInformation.newestVersion });
		}
		if (trackingTag) {
			return m.upgrade_update_tag({ tag: trackingTag });
		}
		return m.update_center_install();
	});

	const installingTitle = $derived.by(() => {
		if (versionInformation?.newestVersion) return m.update_center_installing({ version: versionInformation.newestVersion });
		if (trackingTag) return m.upgrade_update_tag({ tag: trackingTag });
		return m.upgrade_in_progress();
	});

	const effectiveReleaseNotes = $derived(versionInformation?.releaseNotes?.trim() || debugRelease?.body?.trim() || '');
	const effectiveReleasedAt = $derived(versionInformation?.releasedAt || debugRelease?.publishedAt || '');
	const effectiveReleaseUrl = $derived(versionInformation?.releaseUrl || debugRelease?.url || '');

	const releasedAgo = $derived.by(() => {
		const at = effectiveReleasedAt;
		return at ? formatRelativeTime(at) : '';
	});

	let jobId = $state<string | null>(null);
	let startAttempt = 0;

	const job = $derived(upgradeJobStore.job?.id === jobId ? upgradeJobStore.job : null);
	const result = $derived(job?.results?.find((row) => row.environmentId === targetEnvironmentId));
	const finished = $derived(!!job && !isActiveUpgradeJob(job));
	const succeeded = $derived(result?.status === 'updated' || result?.status === 'up_to_date');

	$effect(() => {
		if (!open) return;
		return upgradeJobStore.watch();
	});

	function includesTarget(candidate: UpgradeJob | null): candidate is UpgradeJob {
		return isActiveUpgradeJob(candidate) && !!candidate.results?.some((row) => row.environmentId === targetEnvironmentId);
	}

	async function handleConfirm() {
		const attempt = ++startAttempt;
		upgrading = true;

		const startResult = await tryCatch(systemUpgradeService.startUpgradeJob([targetEnvironmentId]));
		let next = startResult.data;
		if (startResult.error instanceof APIError && startResult.error.status === 409) {
			const active = await upgradeJobStore.load();
			if (includesTarget(active)) next = active;
		}
		if (attempt !== startAttempt) return;

		if (!next) {
			upgrading = false;
			await handleApiResultWithCallbacks({ result: startResult, message: m.upgrade_start_failed() });
			return;
		}

		upgradeJobStore.follow(next);
		jobId = next.id;
	}

	function resetState() {
		startAttempt++;
		jobId = null;
		upgrading = false;
	}

	async function handleClose() {
		const wasFinished = finished;
		resetState();
		open = false;
		if (wasFinished) await onFinished?.();
	}
</script>

<Dialog.Root
	{open}
	onOpenChange={(nextOpen) => {
		if (nextOpen) open = true;
		else void handleClose();
	}}
>
	<Dialog.Content sectioned class="flex max-h-(--max-height-dscreen-90) flex-col overflow-hidden sm:max-w-140">
		{#if upgrading}
			<div class="px-6 pt-6 pb-2">
				<Dialog.Header>
					<Dialog.Title>{installingTitle}</Dialog.Title>
				</Dialog.Header>
			</div>

			<div class="min-h-0 flex-1 overflow-y-auto border-t border-border/60">
				{#if result}
					<ul>
						<UpgradeJobRow {result} logsOpen />
					</ul>
				{:else}
					<div class="flex items-center gap-2 px-6 py-4 text-sm text-muted-foreground">
						<Spinner class="size-4" />
						<span>{m.environments_update_all_loading_status()}</span>
					</div>
				{/if}
			</div>

			<div class="px-6 pb-6">
				{#if finished}
					<div class="mt-4 flex items-center justify-between rounded-lg border border-border/60 px-3 py-2.5">
						{#if succeeded}
							<p class="flex items-center gap-2 text-sm font-medium text-success">
								<SuccessIcon class="size-4" />
								{m.update_center_complete()}
							</p>
						{:else}
							<p class="flex min-w-0 items-center gap-2 text-sm font-medium text-destructive">
								<AlertTriangleIcon class="size-4 shrink-0" />
								<span class="truncate">{result?.error || job?.error || m.environments_update_all_finished_with_issues()}</span>
							</p>
						{/if}
						<ArcaneButton action="base" onclick={handleClose} size="sm" customLabel={m.common_close()} />
					</div>
				{:else}
					<p class="mt-4 text-xs text-muted-foreground">
						{m.update_center_estimated()}
					</p>
				{/if}
			</div>
		{:else}
			<div class="px-6 pt-6 pb-4">
				<Dialog.Header>
					<div class="space-y-3">
						<Dialog.Title>
							{#if isRemoteEnvironment && versionInformation?.newestVersion}
								{m.upgrade_remote_description({
									targetDescription: environmentName ?? '',
									version: versionInformation.newestVersion
								})}
							{:else if isRemoteEnvironment}
								{m.update_center_remote_title({ target: environmentName ?? '' })}
							{:else if !isSemver && trackingTag}
								{m.upgrade_update_tag({ tag: trackingTag })}
							{:else}
								{m.update_center_title()}
							{/if}
						</Dialog.Title>

						<VersionUpdateSummary {versionInformation} {newestVersion} {releasedAgo} />
					</div>
				</Dialog.Header>
			</div>

			<div class="flex min-h-0 flex-1 flex-col border-t border-border/60">
				<div class="flex items-center justify-between px-6 pt-4 pb-2">
					<h3 class="text-sm font-semibold text-foreground">{m.update_center_whats_new()}</h3>
					{#if effectiveReleaseUrl}
						<a
							href={effectiveReleaseUrl}
							target="_blank"
							rel="noopener noreferrer"
							class="inline-flex items-center gap-1 text-xs text-muted-foreground transition-colors hover:text-foreground"
						>
							{m.update_center_view_full_release()}
							<ExternalLinkIcon class="size-3" />
						</a>
					{/if}
				</div>

				<div class="min-h-0 flex-1 overflow-y-auto px-6 pb-4">
					{#if effectiveReleaseNotes}
						<ReleaseNotes markdown={effectiveReleaseNotes} />
					{:else if debug && debugFetching}
						<div class="flex items-center gap-2 text-sm text-muted-foreground">
							<Spinner class="size-3.5" />
							<span>Loading release notes…</span>
						</div>
					{:else if debug && debugFetchSettled && !effectiveReleaseNotes}
						<p class="text-sm text-muted-foreground italic">
							{m.update_center_release_notes_unavailable()}
						</p>
					{:else}
						<p class="text-sm text-muted-foreground italic">
							{m.update_center_release_notes_unavailable()}
						</p>
					{/if}
				</div>
			</div>

			<div class="border-t border-border/60 bg-muted/30 px-6 py-3">
				<p class="text-xs leading-relaxed text-muted-foreground">
					{m.update_center_summary()}
				</p>
			</div>

			<Dialog.Footer>
				<ArcaneButton action="cancel" customLabel={m.update_center_later()} onclick={() => (open = false)} />
				{#if canInstall}
					<ArcaneButton action="update" customLabel={installLabel} onclick={handleConfirm} />
				{/if}
			</Dialog.Footer>
		{/if}
	</Dialog.Content>
</Dialog.Root>
