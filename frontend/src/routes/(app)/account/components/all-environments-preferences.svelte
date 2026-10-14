<script lang="ts">
	import { toast } from 'svelte-sonner';

	import AccentColorPicker from '#lib/components/accent-color/accent-color-picker.svelte';
	import { ArcaneButton } from '#lib/components/arcane-button/index.js';
	import SelectWithLabel from '#lib/components/form/select-with-label.svelte';
	import SettingsRow from '#lib/components/settings/settings-row.svelte';
	import SettingsSection from '#lib/components/settings/settings-section.svelte';
	import * as Alert from '#lib/components/ui/alert/index.js';
	import { Input } from '#lib/components/ui/input/index.js';
	import { Switch } from '#lib/components/ui/switch/index.js';
	import { AlertTriangleIcon, ResetIcon } from '#lib/icons/index.js';
	import { m } from '#lib/paraglide/messages.js';
	import { settingsService } from '#lib/services/settings-service.js';
	import settingsStore from '#lib/stores/config-store.svelte.js';
	import {
		environmentColor,
		environmentColorPalette,
		withEnvironmentColor
	} from '#lib/stores/environment-colors.store.svelte.js';
	import { allEnvironmentsPreview, environmentStore } from '#lib/stores/environment.store.svelte.js';
	import { featureStore } from '#lib/stores/features.store.svelte.js';
	import userStore from '#lib/stores/user-store.svelte.js';
	import type { EnvironmentLabelStyle } from '#lib/types/auth.js';
	import { handleApiResultWithCallbacks } from '#lib/utils/api.js';
	import { isGlobalAdmin } from '#lib/utils/auth.js';
	import { tryCatch } from '#lib/utils/try-catch.js';
	import { saveUserPreferences } from '#lib/utils/user-preferences.js';

	const environments = $derived(environmentStore.available.filter((env) => env.enabled));
	const swatches = environmentColorPalette.map((color) => ({ label: color, color }));

	const labelStyle = $derived(userStore.current?.preferences?.environmentLabelStyle ?? 'name');
	const labelStyleOptions = [
		{ value: 'name', label: m.environment_label_style_name() },
		{ value: 'dot', label: m.environment_label_style_dot() }
	];

	function pickColor(environmentId: string, color: string | null) {
		const environmentColors = withEnvironmentColor(environmentId, color);
		if (environmentColors) void saveUserPreferences({ environmentColors });
		else toast.info(m.environment_color_in_use());
	}

	const environmentLimit = $derived(Number(settingsStore.current?.allEnvironmentsLimit) || 64);
	let environmentLimitInput = $derived(String(environmentLimit));
	let environmentLimitError = $state<string | null>(null);

	async function commitEnvironmentLimit() {
		const parsed = Number(environmentLimitInput);
		if (!Number.isInteger(parsed) || parsed < 2 || parsed > 256) {
			environmentLimitError = m.all_environments_limit_help();
			return;
		}
		environmentLimitError = null;
		if (parsed === environmentLimit) return;
		await handleApiResultWithCallbacks({
			result: await tryCatch(settingsService.updateSettings({ allEnvironmentsLimit: parsed })),
			message: m.common_update_failed({ resource: m.settings() }),
			onSuccess: (updated) => {
				settingsStore.set(updated);
				toast.success(m.common_update_success({ resource: m.settings() }));
			}
		});
	}

	async function setPreview(enabled: boolean) {
		await saveUserPreferences({ allEnvironmentsPreview: enabled });
		// Navigation gates on features in any shown environment.
		for (const env of environmentStore.shown) void featureStore.load(env.id);
	}
</script>

<div class="space-y-6">
	<Alert.Root variant="warning-subtle" icon={AlertTriangleIcon}>
		<Alert.Description>{m.previews_description()}</Alert.Description>
	</Alert.Root>

	<SettingsSection title={m.all_environments()}>
		<SettingsRow
			label={m.all_environments_preview()}
			description={m.all_environments_preview_description()}
			layout="switch"
			for="account-all-environments-preview"
		>
			<Switch
				id="account-all-environments-preview"
				checked={allEnvironmentsPreview.current}
				onCheckedChange={(checked) => void setPreview(checked)}
			/>
		</SettingsRow>
		{#if isGlobalAdmin()}
			<SettingsRow
				for="account-all-environments-limit"
				label={m.all_environments_limit()}
				description={m.all_environments_limit_description()}
				helpText={m.all_environments_limit_help()}
				error={environmentLimitError}
			>
				<Input
					id="account-all-environments-limit"
					type="number"
					min="2"
					max="256"
					bind:value={environmentLimitInput}
					placeholder="64"
					aria-invalid={Boolean(environmentLimitError)}
					onblur={() => void commitEnvironmentLimit()}
				/>
			</SettingsRow>
		{/if}
		{#if allEnvironmentsPreview.current}
			<SettingsRow label={m.environment_label_style()} description={m.environment_label_style_description()} layout="switch">
				<div class="w-52">
					<SelectWithLabel
						id="account-environment-label-style"
						label={m.environment_label_style()}
						hideLabel
						triggerSize="sm"
						value={labelStyle}
						options={labelStyleOptions}
						onValueChange={(value) => void saveUserPreferences({ environmentLabelStyle: value as EnvironmentLabelStyle })}
					/>
				</div>
			</SettingsRow>
			<SettingsRow label={m.environment_colors()} description={m.environment_colors_description()} layout="wide">
				<div class="grid gap-4">
					{#each environments as env (env.id)}
						{@const color = environmentColor(env.id) ?? ''}
						<div class="grid gap-2">
							<div class="flex items-center justify-between gap-3">
								<span class="min-w-0 truncate text-sm">{env.name}</span>
								<ArcaneButton
									action="base"
									tone="ghost"
									size="icon"
									icon={ResetIcon}
									showLabel={false}
									customLabel={m.common_reset()}
									onclick={() => pickColor(env.id, null)}
								/>
							</div>
							<!-- Controlled by the stored color, so a pick already used by another environment is not shown as selected. -->
							<AccentColorPicker
								bind:selectedColor={() => color, (next) => pickColor(env.id, next)}
								previousColor={color}
								colors={swatches}
								withTheme={false}
								customColorTitle={m.environment_colors()}
							/>
						</div>
					{/each}
				</div>
			</SettingsRow>
		{/if}
	</SettingsSection>
</div>
