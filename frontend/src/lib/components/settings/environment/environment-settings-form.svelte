<script lang="ts">
	import type { Snippet } from 'svelte';
	import { untrack } from 'svelte';
	import { toast } from 'svelte-sonner';
	import { z } from 'zod/v4';

	import type { useEnvironmentSettingsScope } from '#lib/hooks/use-environment-settings-scope.svelte.js';
	import { m } from '#lib/paraglide/messages.js';
	import type { SettingKey } from '#lib/types/settings.js';
	import type { MixedSettingValue } from '#lib/types/ui/settings.js';
	import { changedFields } from '#lib/utils/payload.js';
	import { createSettingsForm } from '#lib/utils/settings-form.js';

	import { environmentSettingsSchema, requireTrivyServerUrl, type EnvironmentSettingsValues } from './environment-form-schema';
	import type { EnvironmentSettingsContent } from './tab-props';

	type SettingsFormKey = keyof EnvironmentSettingsValues;

	let {
		scope,
		keys,
		content,
		readOnly
	}: {
		scope: ReturnType<typeof useEnvironmentSettingsScope>;
		keys: SettingsFormKey[];
		readOnly: boolean;
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
			(typeof values[key as SettingKey] === 'string' || Array.isArray(values[key as SettingKey])) &&
			key !== 'trivyNetwork' &&
			!(environmentSettingsSchema.shape[key] instanceof z.ZodEnum)
	);
	const nullMixedKeys = mixedKeys.filter((key) => !blankMixedKeys.includes(key));

	const currentSettings = Object.fromEntries(
		formKeys.map((key) => {
			if (nullMixedKeys.includes(key)) return [key, null];
			if (blankMixedKeys.includes(key) || key === 'depotToken') return [key, Array.isArray(values[key as SettingKey]) ? [] : ''];
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
		onReset: () => toast.info(m.changes_reset()),
		readOnly: untrack(() => readOnly)
	});

	function mixed(key: SettingKey): MixedSettingValue[] | undefined {
		const formKey = key as SettingsFormKey;
		if (!mixedKeys.includes(formKey) || JSON.stringify(formInputs[formKey]?.value) !== JSON.stringify(currentSettings[formKey]))
			return undefined;
		return [...Map.groupBy(scope.entries, (entry) => JSON.stringify(entry.settings.values[key]))].map(([, entries]) => {
			const value = entries[0]!.settings.values[key];
			let label = String(value ?? '') || m.not_set();
			if (typeof value === 'boolean') label = value ? m.common_enabled() : m.common_disabled();
			if (Array.isArray(value)) label = value.map((entry) => entry.path).join(', ') || m.not_set();
			if (settings.meta[key]?.sensitive) label = m.hidden();
			return {
				label,
				environments: entries.map((entry) => entry.environment.name),
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
