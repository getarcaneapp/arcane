<script lang="ts">
	import { toast } from 'svelte-sonner';

	import AccentColorPicker from '#lib/components/accent-color/accent-color-picker.svelte';
	import { ArcaneButton } from '#lib/components/arcane-button/index.js';
	import SelectWithLabel from '#lib/components/form/select-with-label.svelte';
	import SettingsRow from '#lib/components/settings/settings-row.svelte';
	import SettingsSection from '#lib/components/settings/settings-section.svelte';
	import { ResetIcon } from '#lib/icons/index.js';
	import { m } from '#lib/paraglide/messages.js';
	import {
		environmentColor,
		environmentColorPalette,
		withEnvironmentColor
	} from '#lib/stores/environment-colors.store.svelte.js';
	import { environmentStore } from '#lib/stores/environment.store.svelte.js';
	import userStore from '#lib/stores/user-store.svelte.js';
	import type { EnvironmentLabelStyle } from '#lib/types/auth.js';
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
</script>

<SettingsSection title={m.environments_title()}>
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
</SettingsSection>
