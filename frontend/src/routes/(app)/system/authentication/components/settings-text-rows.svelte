<script lang="ts" module>
	import type { Settings } from '#lib/types/settings.js';

	export type SettingsTextField<K extends keyof Settings & string = keyof Settings & string> = {
		key: K;
		label: string;
		description?: string;
		helpText?: string;
		placeholder: string;
	};
</script>

<script lang="ts" generics="TValues extends Record<string, unknown>, TKey extends keyof TValues & keyof Settings & string">
	import SettingsRow from '#lib/components/settings/settings-row.svelte';
	import { Input } from '#lib/components/ui/input/index.js';
	import settingsStore from '#lib/stores/config-store.svelte.js';
	import type { FormInputs } from '#lib/types/ui/form.js';

	let {
		fields,
		formInputs
	}: {
		fields: SettingsTextField<TKey>[];
		formInputs: FormInputs<TValues>;
	} = $props();
</script>

{#each fields as field (field.key)}
	<SettingsRow
		for={field.key}
		label={field.label}
		description={field.description}
		helpText={field.helpText}
		error={formInputs[field.key].error}
		lock={settingsStore.lock(field.key)}
	>
		{#snippet children({ describedBy, errorId })}
			<Input
				id={field.key}
				placeholder={field.placeholder}
				bind:value={formInputs[field.key].value}
				aria-invalid={!!formInputs[field.key].error}
				aria-describedby={describedBy}
				aria-errormessage={errorId}
			/>
		{/snippet}
	</SettingsRow>
{/each}
