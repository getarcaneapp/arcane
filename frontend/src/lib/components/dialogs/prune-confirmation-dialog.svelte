<script lang="ts">
	import { ResponsiveDialog } from '$lib/components/ui/responsive-dialog/index.js';
	import { ArcaneButton } from '$lib/components/arcane-button/index.js';
	import * as Checkbox from '$lib/components/ui/checkbox/index.js';
	import { Label } from '$lib/components/ui/label/index.js';
	import { Spinner } from '$lib/components/ui/spinner/index.js';
	import * as Alert from '$lib/components/ui/alert/index.js';
	import { m } from '$lib/paraglide/messages';
	import { TrashIcon, AlertIcon } from '$lib/icons';

	type PruneType = 'containers' | 'images' | 'networks' | 'volumes' | 'buildCache';

	interface Props {
		open?: boolean;
		isPruning?: boolean;
		imagePruneMode?: 'dangling' | 'all';
		onConfirm?: (selectedTypes: PruneType[]) => void;
		onCancel?: () => void;
	}

	let {
		open = $bindable(),
		isPruning = false,
		imagePruneMode = 'dangling',
		onConfirm = () => {},
		onCancel = () => {}
	}: Props = $props();

	let pruneContainers = $state(true);
	let pruneImages = $state(true);
	let pruneNetworks = $state(true);
	let pruneVolumes = $state(false);
	let pruneBuildCache = $state(false);

	const selectedTypes = $derived.by(() => {
		const types: PruneType[] = [];
		if (pruneContainers) types.push('containers');
		if (pruneImages) types.push('images');
		if (pruneNetworks) types.push('networks');
		if (pruneVolumes) types.push('volumes');
		if (pruneBuildCache) types.push('buildCache');
		return types;
	});

	function handleConfirm() {
		if (selectedTypes.length > 0 && !isPruning) {
			onConfirm(selectedTypes);
		}
	}

	function handleCancel() {
		if (!isPruning) {
			onCancel();
		}
	}
</script>

<ResponsiveDialog
	bind:open
	onOpenChange={(isOpen) => !isOpen && handleCancel()}
	title={m.prune_confirm_system_title()}
	description={m.prune_confirm_description()}
	contentClass="sm:max-w-[450px]"
>
	{#snippet children()}
		<div class="grid gap-4">
			<div class="flex items-center space-x-3">
				<Checkbox.Root id="prune-containers" bind:checked={pruneContainers} disabled={isPruning} />
				<Label
					for="prune-containers"
					class="text-sm leading-none font-medium peer-disabled:cursor-not-allowed peer-disabled:opacity-70"
				>
					{m.prune_stopped_containers()}
				</Label>
			</div>

			<div class="flex items-center space-x-3">
				<Checkbox.Root id="prune-images" bind:checked={pruneImages} disabled={isPruning} />
				<Label
					for="prune-images"
					class="text-sm leading-none font-medium peer-disabled:cursor-not-allowed peer-disabled:opacity-70"
				>
					{m.prune_unused_images()} ({imagePruneMode === 'dangling' ? m.prune_images_mode_dangling() : m.prune_images_mode_all()})
				</Label>
			</div>

			<div class="flex items-center space-x-3">
				<Checkbox.Root id="prune-build-cache" bind:checked={pruneBuildCache} disabled={isPruning} />
				<Label
					for="prune-build-cache"
					class="text-sm leading-none font-medium peer-disabled:cursor-not-allowed peer-disabled:opacity-70"
				>
					{m.build_cache()}
				</Label>
			</div>

			<div class="flex items-center space-x-3">
				<Checkbox.Root id="prune-networks" bind:checked={pruneNetworks} disabled={isPruning} />
				<Label
					for="prune-networks"
					class="text-sm leading-none font-medium peer-disabled:cursor-not-allowed peer-disabled:opacity-70"
				>
					{m.prune_unused_networks()}
				</Label>
			</div>

			<div class="flex items-start space-x-3">
				<Checkbox.Root id="prune-volumes" bind:checked={pruneVolumes} disabled={isPruning} class="mt-1" />
				<div class="grid gap-1.5 leading-none">
					<Label for="prune-volumes" class="text-sm font-medium peer-disabled:cursor-not-allowed peer-disabled:opacity-70">
						{m.prune_unused_volumes()} <span class="text-destructive">{m.prune_potentially_destructive()}</span>
					</Label>
					<p class="text-muted-foreground text-xs">{m.prune_volumes_guidance()}</p>
				</div>
			</div>

			{#if pruneVolumes}
				<Alert.Root variant="destructive" class="mt-2">
					<AlertIcon class="size-4" />
					<Alert.Title>{m.prune_volumes_warning_title()}</Alert.Title>
					<Alert.Description>{m.prune_volumes_warning_description()}</Alert.Description>
				</Alert.Root>
			{/if}
		</div>
	{/snippet}

	{#snippet footer()}
		<ArcaneButton action="cancel" onclick={handleCancel} disabled={isPruning} />
		<ArcaneButton
			action="remove"
			onclick={handleConfirm}
			disabled={selectedTypes.length === 0 || isPruning}
			loading={isPruning}
			customLabel={m.prune_button({ count: selectedTypes.length })}
			loadingLabel={m.common_action_pruning()}
		/>
	{/snippet}
</ResponsiveDialog>
