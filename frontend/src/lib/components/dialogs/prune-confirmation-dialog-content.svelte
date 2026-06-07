<script lang="ts">
	import { ArcaneButton } from '$lib/components/arcane-button/index.js';
	import PruneModeCard from '$lib/components/prune/prune-mode-card.svelte';
	import { m } from '$lib/paraglide/messages';
	import type { SystemPruneRequest } from '$lib/types/prune.type';
	import type { Settings } from '$lib/types/settings.type';

	interface Props {
		defaults?: Settings | null;
		isPruning?: boolean;
		onConfirm?: (request: SystemPruneRequest) => void;
		onCancel?: () => void;
	}

	let { defaults = null, isPruning = false, onConfirm = () => {}, onCancel = () => {} }: Props = $props();

	function getInitialSelectionInternal() {
		return {
			containerMode: defaults?.pruneContainerMode ?? 'stopped',
			containerUntil: defaults?.pruneContainerUntil ?? '',
			imageMode: defaults?.pruneImageMode ?? 'dangling',
			imageUntil: defaults?.pruneImageUntil ?? '',
			networkMode: defaults?.pruneNetworkMode ?? 'unused',
			networkUntil: defaults?.pruneNetworkUntil ?? '',
			volumeMode: defaults?.pruneVolumeMode ?? 'none',
			buildCacheMode: defaults?.pruneBuildCacheMode ?? 'none',
			buildCacheUntil: defaults?.pruneBuildCacheUntil ?? ''
		} as const;
	}

	const initialSelectionInternal = getInitialSelectionInternal();

	let containerMode = $state<'none' | 'stopped' | 'olderThan'>(initialSelectionInternal.containerMode);
	let containerUntil = $state(initialSelectionInternal.containerUntil);
	let imageMode = $state<'none' | 'dangling' | 'all' | 'olderThan'>(initialSelectionInternal.imageMode);
	let imageUntil = $state(initialSelectionInternal.imageUntil);
	let networkMode = $state<'none' | 'unused' | 'olderThan'>(initialSelectionInternal.networkMode);
	let networkUntil = $state(initialSelectionInternal.networkUntil);
	let volumeMode = $state<'none' | 'anonymous' | 'all'>(initialSelectionInternal.volumeMode);
	let buildCacheMode = $state<'none' | 'unused' | 'all' | 'olderThan'>(initialSelectionInternal.buildCacheMode);
	let buildCacheUntil = $state(initialSelectionInternal.buildCacheUntil);

	const containerModes = [
		{ value: 'none', label: m.prune_mode_none() },
		{ value: 'stopped', label: m.prune_stopped_containers() },
		{ value: 'olderThan', label: m.prune_mode_older_than() }
	];
	const imageModes = [
		{ value: 'none', label: m.prune_mode_none() },
		{ value: 'dangling', label: m.prune_images_mode_dangling() },
		{ value: 'all', label: m.prune_images_mode_all() },
		{ value: 'olderThan', label: m.prune_mode_older_than() }
	];
	const networkModes = [
		{ value: 'none', label: m.prune_mode_none() },
		{ value: 'unused', label: m.prune_unused_networks() },
		{ value: 'olderThan', label: m.prune_mode_older_than() }
	];
	const volumeModes = [
		{ value: 'none', label: m.prune_mode_none() },
		{ value: 'anonymous', label: m.prune_volumes_mode_anonymous(), destructive: true },
		{ value: 'all', label: m.prune_volumes_mode_all(), destructive: true }
	];
	const buildCacheModes = [
		{ value: 'none', label: m.prune_mode_none() },
		{ value: 'unused', label: m.prune_build_cache_mode_unused() },
		{ value: 'all', label: m.prune_build_cache_mode_all() },
		{ value: 'olderThan', label: m.prune_mode_older_than() }
	];

	function buildPruneRequestInternal(): SystemPruneRequest {
		const request: SystemPruneRequest = {};
		if (containerMode !== 'none') {
			request.containers = {
				mode: containerMode,
				...(containerMode === 'olderThan' ? { until: containerUntil } : {})
			};
		}
		if (imageMode !== 'none') {
			request.images = {
				mode: imageMode,
				...(imageMode === 'olderThan' ? { until: imageUntil } : {})
			};
		}
		if (networkMode !== 'none') {
			request.networks = {
				mode: networkMode,
				...(networkMode === 'olderThan' ? { until: networkUntil } : {})
			};
		}
		if (volumeMode !== 'none') {
			request.volumes = { mode: volumeMode };
		}
		if (buildCacheMode !== 'none') {
			request.buildCache = {
				mode: buildCacheMode,
				...(buildCacheMode === 'olderThan' ? { until: buildCacheUntil } : {})
			};
		}
		return request;
	}

	let selectedCountInternal = $derived(Object.keys(buildPruneRequestInternal()).length);

	function handleConfirmInternal() {
		const pruneRequest = buildPruneRequestInternal();
		if (Object.keys(pruneRequest).length > 0 && !isPruning) {
			onConfirm(pruneRequest);
		}
	}

	function handleCancelInternal() {
		if (!isPruning) {
			onCancel();
		}
	}
</script>

<div class="space-y-4">
	<div class="grid gap-2 md:grid-cols-2">
		<PruneModeCard
			title={m.prune_containers_label()}
			description={m.scheduled_prune_containers_description()}
			modeOptions={containerModes}
			bind:value={containerMode}
			bind:untilValue={containerUntil}
			disabled={isPruning}
		/>
		<PruneModeCard
			title={m.prune_images_label()}
			description={m.prune_images_dialog_description()}
			modeOptions={imageModes}
			bind:value={imageMode}
			bind:untilValue={imageUntil}
			disabled={isPruning}
		/>
		<PruneModeCard
			title={m.prune_networks_label()}
			description={m.scheduled_prune_networks_description()}
			modeOptions={networkModes}
			bind:value={networkMode}
			bind:untilValue={networkUntil}
			disabled={isPruning}
		/>
		<PruneModeCard
			title={m.prune_volumes_label()}
			description={m.prune_volumes_guidance()}
			modeOptions={volumeModes}
			bind:value={volumeMode}
			disabled={isPruning}
			warningTitle={m.prune_volumes_warning_title()}
			warningDescription={volumeMode === 'all'
				? m.prune_volumes_warning_description_all()
				: m.prune_volumes_warning_description()}
		/>
		<div class="md:col-span-2">
			<PruneModeCard
				title={m.prune_build_cache_label()}
				description={m.scheduled_prune_build_cache_description()}
				modeOptions={buildCacheModes}
				bind:value={buildCacheMode}
				bind:untilValue={buildCacheUntil}
				disabled={isPruning}
			/>
		</div>
	</div>

	<div class="grid grid-cols-2 gap-3">
		<ArcaneButton action="cancel" onclick={handleCancelInternal} disabled={isPruning} class="w-full" />
		<ArcaneButton
			action="remove"
			onclick={handleConfirmInternal}
			disabled={selectedCountInternal === 0 || isPruning}
			loading={isPruning}
			customLabel={m.prune_button({ count: selectedCountInternal })}
			loadingLabel={m.common_action_pruning()}
			class="w-full"
		/>
	</div>
</div>
