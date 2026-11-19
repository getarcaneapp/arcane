<script lang="ts">
	import { ArcaneButton } from '#lib/components/arcane-button/index.js';
	import PruneModePicker from '#lib/components/prune/prune-mode-picker.svelte';
	import SettingsRow from '#lib/components/settings/settings-row.svelte';
	import SettingsSection from '#lib/components/settings/settings-section.svelte';
	import { m } from '#lib/paraglide/messages.js';
	import type { Settings } from '#lib/types/settings.js';
	import type { PruneAllRequest } from '#lib/types/system.js';

	interface Props {
		defaults?: Partial<Settings> | null;
		isPruning?: boolean;
		onConfirm?: (request: PruneAllRequest) => void;
		onCancel?: () => void;
	}

	let { defaults = null, isPruning = false, onConfirm = () => {}, onCancel = () => {} }: Props = $props();

	function getInitialSelection() {
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

	const initialSelection = getInitialSelection();

	let containerMode = $state<'none' | 'stopped' | 'olderThan'>(initialSelection.containerMode);
	let containerUntil = $state(initialSelection.containerUntil);
	let imageMode = $state<'none' | 'dangling' | 'all' | 'olderThan'>(initialSelection.imageMode);
	let imageUntil = $state(initialSelection.imageUntil);
	let networkMode = $state<'none' | 'unused' | 'olderThan'>(initialSelection.networkMode);
	let networkUntil = $state(initialSelection.networkUntil);
	let volumeMode = $state<'none' | 'anonymous' | 'all'>(initialSelection.volumeMode);
	let buildCacheMode = $state<'none' | 'unused' | 'all' | 'olderThan'>(initialSelection.buildCacheMode);
	let buildCacheUntil = $state(initialSelection.buildCacheUntil);

	const containerModes = [
		{ value: 'none', label: m.none() },
		{ value: 'stopped', label: m.prune_stopped_containers() },
		{ value: 'olderThan', label: m.prune_mode_older_than() }
	];
	const imageModes = [
		{ value: 'none', label: m.none() },
		{ value: 'dangling', label: m.prune_images_mode_dangling() },
		{ value: 'all', label: m.all_unused() },
		{ value: 'olderThan', label: m.prune_mode_older_than() }
	];
	const networkModes = [
		{ value: 'none', label: m.none() },
		{ value: 'unused', label: m.unused_networks() },
		{ value: 'olderThan', label: m.prune_mode_older_than() }
	];
	const volumeModes = [
		{ value: 'none', label: m.none() },
		{ value: 'anonymous', label: m.prune_volumes_mode_anonymous(), destructive: true },
		{ value: 'all', label: m.all_unused(), destructive: true }
	];
	const buildCacheModes = [
		{ value: 'none', label: m.none() },
		{ value: 'unused', label: m.prune_build_cache_mode_unused() },
		{ value: 'all', label: m.prune_build_cache_mode_all() },
		{ value: 'olderThan', label: m.prune_mode_older_than() }
	];

	function buildPruneRequest(): PruneAllRequest {
		const request: PruneAllRequest = {};
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

	let selectedCount = $derived(Object.keys(buildPruneRequest()).length);

	function handleConfirm() {
		const pruneRequest = buildPruneRequest();
		if (Object.keys(pruneRequest).length > 0 && !isPruning) {
			onConfirm(pruneRequest);
		}
	}

	function handleCancel() {
		if (!isPruning) {
			onCancel();
		}
	}
</script>

<div class="space-y-4">
	<SettingsSection>
		<SettingsRow label={m.containers()} description={m.scheduled_prune_containers_description()} layout="wide">
			{#snippet children({ describedBy })}
				<PruneModePicker
					modeOptions={containerModes}
					bind:value={containerMode}
					bind:untilValue={containerUntil}
					disabled={isPruning}
					aria-describedby={describedBy}
				/>
			{/snippet}
		</SettingsRow>
		<SettingsRow label={m.images()} description={m.prune_images_dialog_description()} layout="wide">
			{#snippet children({ describedBy })}
				<PruneModePicker
					modeOptions={imageModes}
					bind:value={imageMode}
					bind:untilValue={imageUntil}
					disabled={isPruning}
					aria-describedby={describedBy}
				/>
			{/snippet}
		</SettingsRow>
		<SettingsRow label={m.resource_networks_cap()} description={m.scheduled_prune_networks_description()} layout="wide">
			{#snippet children({ describedBy })}
				<PruneModePicker
					modeOptions={networkModes}
					bind:value={networkMode}
					bind:untilValue={networkUntil}
					disabled={isPruning}
					aria-describedby={describedBy}
				/>
			{/snippet}
		</SettingsRow>
		<SettingsRow label={m.resource_volumes_cap()} description={m.prune_volumes_guidance()} layout="wide">
			{#snippet children({ describedBy })}
				<PruneModePicker
					modeOptions={volumeModes}
					bind:value={volumeMode}
					disabled={isPruning}
					warningTitle={m.prune_volumes_warning_title()}
					warningDescription={volumeMode === 'all'
						? m.prune_volumes_warning_description_all()
						: m.prune_volumes_warning_description()}
					aria-describedby={describedBy}
				/>
			{/snippet}
		</SettingsRow>
		<SettingsRow label={m.build_cache()} description={m.scheduled_prune_build_cache_description()} layout="wide">
			{#snippet children({ describedBy })}
				<PruneModePicker
					modeOptions={buildCacheModes}
					bind:value={buildCacheMode}
					bind:untilValue={buildCacheUntil}
					disabled={isPruning}
					aria-describedby={describedBy}
				/>
			{/snippet}
		</SettingsRow>
	</SettingsSection>

	<div class="grid grid-cols-2 gap-3">
		<ArcaneButton action="cancel" onclick={handleCancel} disabled={isPruning} class="w-full" />
		<ArcaneButton
			action="remove"
			onclick={handleConfirm}
			disabled={selectedCount === 0 || isPruning}
			loading={isPruning}
			customLabel={m.prune_button({ count: selectedCount })}
			loadingLabel={m.common_action_pruning()}
			class="w-full"
		/>
	</div>
</div>
