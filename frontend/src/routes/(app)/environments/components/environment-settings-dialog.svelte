<script lang="ts">
	import { toast } from 'svelte-sonner';

	import SettingsRow from '#lib/components/settings/settings-row.svelte';
	import SheetFooterActions from '#lib/components/sheets/sheet-footer-actions.svelte';
	import { Input } from '#lib/components/ui/input/index.js';
	import * as ResponsiveDialog from '#lib/components/ui/responsive-dialog/index.js';
	import { m } from '#lib/paraglide/messages.js';
	import settingsStore from '#lib/stores/config-store.svelte.js';
	import { handleApiResultWithCallbacks } from '#lib/utils/api.js';
	import { preventDefault } from '#lib/utils/settings.svelte.js';
	import { tryCatch } from '#lib/utils/try-catch.js';

	type Props = {
		open: boolean;
	};

	let { open = $bindable(false) }: Props = $props();

	const environmentLimit = $derived(Number(settingsStore.current?.allEnvironmentsLimit) || 64);
	let environmentLimitInput = $derived(String(environmentLimit));
	let environmentLimitError = $state<string | null>(null);
	let isSaving = $state(false);

	function handleOpenChange(nextOpen: boolean) {
		if (!nextOpen && isSaving) return;
		if (!nextOpen) {
			environmentLimitInput = String(environmentLimit);
			environmentLimitError = null;
		}
		open = nextOpen;
	}

	async function handleSubmit() {
		const parsed = Number(environmentLimitInput);
		if (!Number.isInteger(parsed) || parsed < 2 || parsed > 256) {
			environmentLimitError = m.all_environments_limit_help();
			return;
		}
		environmentLimitError = null;
		if (parsed === environmentLimit) {
			handleOpenChange(false);
			return;
		}
		await handleApiResultWithCallbacks({
			result: await tryCatch(settingsStore.save({ allEnvironmentsLimit: parsed })),
			message: m.common_update_failed({ resource: m.settings() }),
			setLoadingState: (value) => (isSaving = value),
			onSuccess: () => {
				toast.success(m.common_update_success({ resource: m.settings() }));
				handleOpenChange(false);
			}
		});
	}
</script>

<ResponsiveDialog.Root {open} onOpenChange={handleOpenChange} title={m.settings()} contentClass="sm:max-w-130">
	{#snippet children()}
		<form onsubmit={preventDefault(handleSubmit)} class="grid gap-4 py-4">
			<SettingsRow
				for="environments-all-environments-limit"
				label={m.all_environments_limit()}
				description={m.all_environments_limit_description()}
				helpText={m.all_environments_limit_help()}
				error={environmentLimitError}
				layout="wide"
				class="p-0"
			>
				<Input
					id="environments-all-environments-limit"
					type="number"
					min="2"
					max="256"
					bind:value={environmentLimitInput}
					placeholder="64"
					aria-invalid={Boolean(environmentLimitError)}
				/>
			</SettingsRow>
		</form>
	{/snippet}

	{#snippet footer()}
		<SheetFooterActions
			onCancel={() => handleOpenChange(false)}
			submitAction="save"
			submitLoading={isSaving}
			submitDisabled={isSaving}
			onSubmit={handleSubmit}
		/>
	{/snippet}
</ResponsiveDialog.Root>
