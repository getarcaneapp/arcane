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
	const parallelUpgrades = $derived(Number(settingsStore.current?.upgradeMaxParallelAgents) || 4);
	let environmentLimitInput = $derived(String(environmentLimit));
	let parallelUpgradesInput = $derived(String(parallelUpgrades));
	let environmentLimitError = $state<string | null>(null);
	let parallelUpgradesError = $state<string | null>(null);
	let isSaving = $state(false);

	function handleOpenChange(nextOpen: boolean) {
		if (!nextOpen && isSaving) return;
		if (!nextOpen) {
			environmentLimitInput = String(environmentLimit);
			parallelUpgradesInput = String(parallelUpgrades);
			environmentLimitError = null;
			parallelUpgradesError = null;
		}
		open = nextOpen;
	}

	async function handleSubmit() {
		const parsedLimit = Number(environmentLimitInput);
		const parsedParallel = Number(parallelUpgradesInput);
		const limitValid = Number.isInteger(parsedLimit) && parsedLimit >= 2 && parsedLimit <= 256;
		const parallelValid = Number.isInteger(parsedParallel) && parsedParallel >= 1 && parsedParallel <= 16;
		environmentLimitError = limitValid ? null : m.all_environments_limit_help();
		parallelUpgradesError = parallelValid ? null : m.upgrade_max_parallel_agents_help();
		if (!limitValid || !parallelValid) return;
		if (parsedLimit === environmentLimit && parsedParallel === parallelUpgrades) {
			handleOpenChange(false);
			return;
		}
		await handleApiResultWithCallbacks({
			result: await tryCatch(settingsStore.save({ allEnvironmentsLimit: parsedLimit, upgradeMaxParallelAgents: parsedParallel })),
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
			<SettingsRow
				for="environments-upgrade-max-parallel-agents"
				label={m.upgrade_max_parallel_agents()}
				description={m.upgrade_max_parallel_agents_description()}
				helpText={m.upgrade_max_parallel_agents_help()}
				error={parallelUpgradesError}
				lock={settingsStore.lock('upgradeMaxParallelAgents')}
				layout="wide"
				class="p-0"
			>
				<Input
					id="environments-upgrade-max-parallel-agents"
					type="number"
					min="1"
					max="16"
					bind:value={parallelUpgradesInput}
					placeholder="4"
					disabled={settingsStore.isLocked('upgradeMaxParallelAgents')}
					aria-invalid={Boolean(parallelUpgradesError)}
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
