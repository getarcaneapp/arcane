<script lang="ts">
	import { toast } from 'svelte-sonner';

	import AccentColorPicker from '#lib/components/accent-color/accent-color-picker.svelte';
	import { ArcaneButton } from '#lib/components/arcane-button/index.js';
	import SelectWithLabel from '#lib/components/form/select-with-label.svelte';
	import SettingsRow from '#lib/components/settings/settings-row.svelte';
	import SettingsSection from '#lib/components/settings/settings-section.svelte';
	import * as Popover from '#lib/components/ui/popover/index.js';
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
		<div class="grid gap-1">
			{#each environments as env (env.id)}
				{@const color = environmentColor(env.id) ?? ''}
				{@const disabledColors = environments
					.filter((other) => other.id !== env.id)
					.map((other) => environmentColor(other.id))
					.filter((other): other is string => Boolean(other))}
				<div class="flex items-center gap-3">
					<Popover.Root>
						<Popover.Trigger>
							{#snippet child({ props })}
								<button
									{...props}
									type="button"
									class="size-5 shrink-0 cursor-pointer rounded-full border-2 bg-(--swatch) transition-transform hover:scale-110"
									style:--swatch={color}
									aria-label={`${env.name} ${m.environment_colors()}`}
								></button>
							{/snippet}
						</Popover.Trigger>
						<Popover.Content align="start" class="w-auto max-w-80 overflow-visible">
							<AccentColorPicker
								bind:selectedColor={() => color, (next) => pickColor(env.id, next)}
								previousColor={color}
								colors={swatches}
								withTheme={false}
								customColorTitle={m.environment_colors()}
								{disabledColors}
							/>
						</Popover.Content>
					</Popover.Root>
					<span class="min-w-0 flex-1 truncate text-sm">{env.name}</span>
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
			{/each}
		</div>
	</SettingsRow>
</SettingsSection>
