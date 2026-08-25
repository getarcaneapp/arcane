<script lang="ts">
	import * as Alert from '#lib/components/ui/alert/index.js';
	import TextInputWithLabel from '#lib/components/form/text-input-with-label.svelte';
	import SheetFooterActions from '#lib/components/sheets/sheet-footer-actions.svelte';
	import * as ResponsiveDialog from '#lib/components/ui/responsive-dialog/index.js';
	import { AlertTriangleIcon } from '#lib/icons';
	import { m } from '#lib/paraglide/messages';
	import type { VolumeSummaryDto } from '#lib/types/docker';
	import { getManagedByLabel } from '#lib/utils/docker';

	let {
		open = $bindable(false),
		volume,
		isLoading,
		onSubmit
	}: {
		open: boolean;
		volume: VolumeSummaryDto | null;
		isLoading: boolean;
		onSubmit: (newName: string) => void;
	} = $props();

	let newName = $state('');
	let error = $state<string | null>(null);

	const isManaged = $derived(Boolean(getManagedByLabel(volume?.labels)));
	const normalizedName = $derived(newName.trim());
	const canRename = $derived(Boolean(volume && normalizedName && normalizedName !== volume.name));

	$effect(() => {
		if (!open) return;
		newName = volume?.name ?? '';
		error = null;
	});

	function handleOpenChange(isOpen: boolean) {
		if (!isOpen) error = null;
	}

	function submit() {
		if (!normalizedName) {
			error = m.volume_name_required();
			return;
		}
		if (normalizedName === volume?.name) {
			error = m.volumes_rename_same_name();
			return;
		}
		error = null;
		onSubmit(normalizedName);
	}
</script>

<ResponsiveDialog.Root
	bind:open
	onOpenChange={handleOpenChange}
	title={m.volumes_rename_title()}
	description={m.volumes_rename_description()}
	contentClass="sm:max-w-[500px]"
>
	{#snippet children()}
		<form
			id="rename-volume-form"
			onsubmit={(event) => {
				event.preventDefault();
				submit();
			}}
			class="space-y-4 py-4"
		>
			<TextInputWithLabel
				id="rename-volume-name"
				bind:value={newName}
				label={m.volumes_rename_new_name()}
				{error}
				disabled={isLoading}
				autocomplete="off"
			/>

			{#if isManaged}
				<Alert.Root variant="warning">
					<AlertTriangleIcon class="size-4" />
					<Alert.Title>{m.volumes_rename_managed_warning_title()}</Alert.Title>
					<Alert.Description class="text-foreground">{m.volumes_rename_managed_warning_description()}</Alert.Description>
				</Alert.Root>
			{/if}
		</form>
	{/snippet}

	{#snippet footer()}
		<SheetFooterActions
			onCancel={() => (open = false)}
			cancelDisabled={isLoading}
			submitAction="edit"
			submitLabel={m.rename()}
			submitForm="rename-volume-form"
			submitLoading={isLoading}
			submitDisabled={!canRename || isLoading}
		/>
	{/snippet}
</ResponsiveDialog.Root>
