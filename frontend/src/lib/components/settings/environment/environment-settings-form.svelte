<script lang="ts">
	import type { Snippet } from 'svelte';
	import { untrack } from 'svelte';
	import { toast } from 'svelte-sonner';
	import { z } from 'zod/v4';

	import type { useEnvironmentSettingsScope } from '#lib/hooks/use-environment-settings-scope.svelte.js';
	import { m } from '#lib/paraglide/messages.js';
	import type { MixedSettingValue, SettingKey } from '#lib/types/settings.js';
	import { changedFields } from '#lib/utils/payload.js';
	import { createSettingsForm } from '#lib/utils/settings-form.js';

	import { environmentSettingsSchema, requireTrivyServerUrl, type EnvironmentSettingsValues } from './environment-form-schema';
	import type { EnvironmentSettingsContent } from './tab-props';

	type SettingsFormKey = keyof EnvironmentSettingsValues;

	let {
		scope,
		keys,
		content
	}: {
		scope: ReturnType<typeof useEnvironmentSettingsScope>;
		keys: SettingsFormKey[];
		content: Snippet<[EnvironmentSettingsContent, ReturnType<typeof useEnvironmentSettingsScope>]>;
	} = $props();

	const { settings, values, formKeys, mixedKeys } = untrack(() => {
		const settings = scope.settings!;
		const formKeys = keys.filter((key) => key in settings.values);
		return {
			settings,
			values: settings.values,
			formKeys,
			mixedKeys: formKeys.filter((key) => key !== 'depotToken' && scope.mixed(key as SettingKey))
		};
	});

	const depotConfigured = Boolean(
		settings.runtime.depotConfigured || (values.depotProjectId?.trim() && values.depotToken?.trim())
	);

	const blankMixedKeys = mixedKeys.filter(
		(key) =>
			typeof values[key as SettingKey] === 'string' &&
			key !== 'trivyNetwork' &&
			!(environmentSettingsSchema.shape[key] instanceof z.ZodEnum)
	);
	const nullMixedKeys = mixedKeys.filter((key) => !blankMixedKeys.includes(key));

	const currentSettings = Object.fromEntries(
		formKeys.map((key) => {
			if (nullMixedKeys.includes(key)) return [key, null];
			if (blankMixedKeys.includes(key) || key === 'depotToken') return [key, ''];
			if (key === 'buildProvider' && !depotConfigured) return [key, 'local'];
			return [key, values[key as SettingKey]];
		})
	) as EnvironmentSettingsValues;

	const schema = environmentSettingsSchema
		.pick(Object.fromEntries(formKeys.map((key) => [key, true] as const)) as Partial<Record<SettingsFormKey, true>>)
		.extend(Object.fromEntries(nullMixedKeys.map((key) => [key, environmentSettingsSchema.shape[key].nullable()])))
		.superRefine((data, ctx) => {
			if (!mixedKeys.includes('trivyServerUrl')) requireTrivyServerUrl(data, ctx);
		}) as unknown as typeof environmentSettingsSchema;

	const { formInputs } = createSettingsForm({
		schema,
		currentSettings,
		onSave: (data) => scope.save(changedFields(currentSettings, data)),
		onReset: () => toast.info(m.changes_reset())
	});

	function mixed(key: SettingKey): MixedSettingValue[] | undefined {
		const formKey = key as SettingsFormKey;
		if (!mixedKeys.includes(formKey) || formInputs[formKey]?.value !== currentSettings[formKey]) return undefined;
		const groups = new Map<unknown, string[]>();
		for (const { environment, settings: environmentSettings } of scope.entries) {
			const value = environmentSettings.values[key];
			groups.set(value, [...(groups.get(value) ?? []), environment.name]);
		}
		return [...groups].map(([value, environments]) => {
			let label = String(value ?? '') || m.not_set();
			if (typeof value === 'boolean') label = value ? m.common_enabled() : m.common_disabled();
			if (settings.meta[key]?.sensitive) label = m.hidden();
			return {
				label,
				environments,
				apply: () => {
					const input = formInputs[formKey];
					if (input) input.value = value as never;
				}
			};
		});
	}
</script>

{@render content(
	{
		formInputs,
		mixed,
		lock: scope.lock,
		settings,
		environmentId: scope.primaryId
	},
	scope
)}
