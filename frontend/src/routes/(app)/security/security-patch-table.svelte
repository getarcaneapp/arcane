<script lang="ts">
	import ArcaneTable from '#lib/components/arcane-table/arcane-table.svelte';
	import RowActionsMenu from '#lib/components/arcane-table/row-actions-menu.svelte';
	import * as DropdownMenu from '#lib/components/ui/dropdown-menu/index.js';
	import UniversalMobileCard from '#lib/components/arcane-table/cards/universal-mobile-card.svelte';
	import MonoCell from '#lib/components/vulnerability/mono-cell.svelte';
	import { Badge } from '#lib/components/ui/badge';
	import { imageService } from '#lib/services/image-service';
	import { activityToastOptions } from '#lib/utils/activity-toast';
	import { m } from '#lib/paraglide/messages';
	import type { ColumnSpec } from '#lib/components/arcane-table/arcane-table.types.svelte';
	import type { Paginated, SearchPaginationSortRequest } from '#lib/types/shared';
	import type { ImagePatchTargetDto, ImagePatchStatus } from '#lib/types/docker';
	import { ShieldCheckIcon, ImagesIcon, ClockIcon } from '#lib/icons';
	import { formatDateTimeShort } from '#lib/utils/formatting';
	import { environmentStore } from '#lib/stores/environment.store.svelte';
	import { hasPermission } from '#lib/utils/auth';
	import userStore from '#lib/stores/user-store';
	import { toast } from 'svelte-sonner';

	type PatchTargetRow = ImagePatchTargetDto & { id: string };

	let {
		targets = $bindable(),
		requestOptions = $bindable()
	}: {
		targets: Paginated<PatchTargetRow>;
		requestOptions: SearchPaginationSortRequest;
	} = $props();

	const currentEnvId = $derived(environmentStore.selected?.id || '0');
	// Track the user store: hasPermission reads it non-reactively, so without
	// this the derived would cache a pre-hydration false forever.
	const canPatchImage = $derived.by(() => {
		$userStore;
		return hasPermission('images:patch', currentEnvId);
	});

	async function refreshPatchTargets(options: SearchPaginationSortRequest) {
		const response = await imageService.listPatchTargets(options);
		const mapped = { ...response, data: (response.data ?? []).map((t) => ({ ...t, id: t.imageId })) };
		targets = mapped;
		return mapped;
	}

	async function handlePatchImage(item: PatchTargetRow) {
		try {
			// The fixable counts come from the stored scan, so patch from that report.
			const record = await imageService.patchImage(item.imageId, { scanId: item.imageId });
			toast.info(m.images_patch_started({ patchedRef: record.patchedRef }), activityToastOptions(record.activityId));
			await refreshPatchTargets(requestOptions);
		} catch (error) {
			console.error('Failed to patch image:', error);
			toast.error(m.images_patch_failed());
		}
	}

	function patchStatusLabel(status: ImagePatchStatus) {
		switch (status) {
			case 'patching':
				return m.common_running();
			case 'completed':
				return m.common_success();
			case 'failed':
				return m.common_failed();
			default:
				return m.common_unknown();
		}
	}

	function lastPatchTooltip(item: PatchTargetRow) {
		const patch = item.lastPatch;
		if (!patch) return '';
		if (patch.status === 'failed' && patch.error) {
			return patch.error;
		}
		return formatDateTimeShort(patch.createdAt);
	}

	function patchStatusVariant(status: ImagePatchStatus): 'green' | 'red' | 'blue' {
		switch (status) {
			case 'completed':
				return 'green';
			case 'failed':
				return 'red';
			default:
				return 'blue';
		}
	}

	const columns = $derived([
		{ accessorKey: 'imageRef', title: m.common_image(), cell: ImageCell },
		{ accessorKey: 'fixableCount', title: m.security_fixable(), cell: FixableCell },
		{ accessorKey: 'scanTime', title: m.security_last_scanned(), cell: ScannedCell },
		{ id: 'lastPatch', title: m.security_last_patch(), cell: LastPatchCell },
		{ id: 'patchedRef', title: m.security_patched_ref(), cell: PatchedRefCell }
	] satisfies ColumnSpec<PatchTargetRow>[]);

	const mobileFields = [
		{ id: 'imageRef', label: m.common_image(), defaultVisible: true },
		{ id: 'fixableCount', label: m.security_fixable(), defaultVisible: true },
		{ id: 'scanTime', label: m.security_last_scanned(), defaultVisible: true },
		{ id: 'lastPatch', label: m.security_last_patch(), defaultVisible: true },
		{ id: 'patchedRef', label: m.security_patched_ref(), defaultVisible: true }
	];
</script>

{#snippet PatchedRefCell({ item }: { item: PatchTargetRow })}
	{#if item.lastPatch?.status === 'completed'}
		<MonoCell value={item.lastPatch.patchedRef} />
	{:else}
		<span class="text-xs text-muted-foreground">-</span>
	{/if}
{/snippet}

{#snippet ImageCell({ item }: { item: PatchTargetRow })}
	<a class="truncate text-sm font-medium hover:underline" href="/images/{item.imageId}">
		{item.imageRef}
	</a>
{/snippet}

{#snippet FixableCell({ item }: { item: PatchTargetRow })}
	{#if item.fixableCount > 0}
		<span class="text-sm">
			<span class="font-semibold text-foreground tabular-nums">{item.fixableCount}</span>
			<span class="text-xs text-muted-foreground">/ {item.totalCount} CVEs</span>
		</span>
	{:else}
		<span class="text-xs text-muted-foreground">{m.security_no_fixable()}</span>
	{/if}
{/snippet}

{#snippet ScannedCell({ item }: { item: PatchTargetRow })}
	<span class="text-sm text-muted-foreground">{formatDateTimeShort(item.scanTime)}</span>
{/snippet}

{#snippet LastPatchCell({ item }: { item: PatchTargetRow })}
	{#if item.lastPatch}
		<div class="flex items-center gap-2" title={lastPatchTooltip(item)}>
			<Badge variant={patchStatusVariant(item.lastPatch.status)} size="sm" minWidth="20">
				{patchStatusLabel(item.lastPatch.status)}
			</Badge>
			{#if item.lastPatch.status === 'completed'}
				{#if item.lastPatchScan}
					<span class="text-xs {item.lastPatchScan.fixableCount === 0 ? 'text-emerald-500' : 'text-amber-500'}">
						{m.security_fixable_after_patch({ count: item.lastPatchScan.fixableCount })}
					</span>
				{:else}
					<span class="text-xs text-muted-foreground">{m.security_verify_scan_pending()}</span>
				{/if}
			{/if}
		</div>
	{:else}
		<span class="text-xs text-muted-foreground">{m.security_not_patched()}</span>
	{/if}
{/snippet}

{#snippet PatchTargetMobileCard({
	item,
	mobileFieldVisibility
}: {
	item: PatchTargetRow;
	mobileFieldVisibility: Record<string, boolean>;
})}
	<UniversalMobileCard
		{item}
		icon={() => ({ component: ShieldCheckIcon, variant: item.fixableCount > 0 ? 'amber' : 'emerald' })}
		title={(item) => item.imageRef}
		subtitle={(item) =>
			(mobileFieldVisibility['fixableCount'] ?? true)
				? item.fixableCount > 0
					? `${item.fixableCount} / ${item.totalCount} CVEs`
					: m.security_no_fixable()
				: null}
		badges={[
			(item) =>
				(mobileFieldVisibility['lastPatch'] ?? true) && item.lastPatch
					? { variant: patchStatusVariant(item.lastPatch.status), text: patchStatusLabel(item.lastPatch.status) }
					: null
		]}
		fields={[
			{
				label: m.security_last_scanned(),
				getValue: (item) => formatDateTimeShort(item.scanTime),
				icon: ClockIcon,
				iconVariant: 'gray',
				show: mobileFieldVisibility['scanTime'] ?? true
			},
			{
				label: m.security_last_patch(),
				getValue: (item) => item.lastPatch?.patchedRef ?? m.security_not_patched(),
				type: 'mono',
				icon: ImagesIcon,
				iconVariant: 'gray',
				show: mobileFieldVisibility['lastPatch'] ?? true
			}
		]}
	/>
{/snippet}

{#snippet rowActions({ item }: { item: PatchTargetRow })}
	<RowActionsMenu>
		<DropdownMenu.Item onclick={() => handlePatchImage(item)} disabled={item.localOnly || item.fixableCount === 0}>
			<ShieldCheckIcon class="size-4" />
			<div class="flex flex-col items-start">
				<span>{m.images_patch()}</span>
				{#if item.localOnly}
					<span class="text-xs text-muted-foreground">{m.security_local_image_unpatchable()}</span>
				{:else if item.fixableCount === 0}
					<span class="text-xs text-muted-foreground">{m.security_nothing_to_patch()}</span>
				{/if}
			</div>
		</DropdownMenu.Item>
	</RowActionsMenu>
{/snippet}

<ArcaneTable
	persistKey="arcane-security-patch-table"
	items={targets}
	bind:requestOptions
	onRefresh={refreshPatchTargets}
	{columns}
	mobileCard={PatchTargetMobileCard}
	{mobileFields}
	selectionDisabled
	rowActions={canPatchImage ? rowActions : undefined}
/>
