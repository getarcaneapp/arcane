<script lang="ts">
	import FormInput from '#lib/components/form/form-input.svelte';
	import SwitchWithLabel from '#lib/components/form/labeled-switch.svelte';
	import SelectWithLabel from '#lib/components/form/select-with-label.svelte';
	import TextInputWithLabel from '#lib/components/form/text-input-with-label.svelte';
	import EnvironmentMultiSelect from '#lib/components/sheets/environment-multi-select.svelte';
	import { Label } from '#lib/components/ui/label/index.js';
	import { m } from '#lib/paraglide/messages.js';
	import { environmentStore } from '#lib/stores/environment.store.svelte.js';

	import {
		DEFAULT_RESOURCE_LIMIT,
		MAX_RESOURCE_LIMIT,
		RESOURCE_KIND_KEYS,
		RESOURCE_KINDS,
		type ResourceFilterKey
	} from './resource-kinds';
	import type { WidgetFormState } from './widget-sheet.svelte';

	const ANY = 'any';

	let { form }: { form: WidgetFormState } = $props();

	const inputs = $derived(form.inputs);
	const definition = $derived(inputs.resourceKind.value ? RESOURCE_KINDS[inputs.resourceKind.value] : undefined);
	const environments = $derived(environmentStore.available.filter((environment) => environment.enabled));

	function setFilter(key: ResourceFilterKey, value: string) {
		inputs.filters.value = Object.fromEntries([
			...Object.entries(inputs.filters.value).filter(([entry]) => entry !== key),
			...(value && value !== ANY ? [[key, value] satisfies [string, string]] : [])
		]);
	}

	function setKind(value: string) {
		const resourceKind = RESOURCE_KIND_KEYS.find((key) => key === value);
		if (!resourceKind || resourceKind === inputs.resourceKind.value) return;
		const allowed: string[] = RESOURCE_KINDS[resourceKind].filters.filter((key) => key !== 'status');
		inputs.resourceKind.value = resourceKind;
		inputs.filters.value = Object.fromEntries(Object.entries(inputs.filters.value).filter(([key]) => allowed.includes(key)));
	}
</script>

<div class="grid gap-4">
	<FormInput
		label={m.title()}
		type="text"
		placeholder={definition?.label() ?? m.filtered_resources()}
		bind:input={inputs.title}
	/>

	<SelectWithLabel
		id="resource-query-kind"
		label={m.resource_type()}
		placeholder={m.common_select_option()}
		value={inputs.resourceKind.value ?? ''}
		error={inputs.resourceKind.error}
		options={RESOURCE_KIND_KEYS.map((key) => ({ value: key, label: RESOURCE_KINDS[key].label() }))}
		onValueChange={setKind}
	/>

	<div class="space-y-2">
		<div>
			<Label>{m.environments_title()}</Label>
			<p class="mt-0.5 text-xs text-muted-foreground">{m.widget_environments_description()}</p>
		</div>
		<EnvironmentMultiSelect {environments} bind:selected={inputs.environmentIds.value} />
	</div>

	<FormInput label={m.search()} type="text" placeholder={m.common_search()} bind:input={inputs.search} />

	{#if definition}
		<TextInputWithLabel
			id="resource-query-label"
			label={m.label()}
			placeholder={m.label_filter_placeholder()}
			value={inputs.filters.value['label'] ?? ''}
			onChange={(value) => setFilter('label', value.trim())}
		/>

		{#if definition.filters.includes('status')}
			<SelectWithLabel
				id="resource-query-status"
				label={m.common_status()}
				placeholder={m.common_all()}
				value={inputs.filters.value['status'] ?? ANY}
				options={[
					{ value: ANY, label: m.common_all() },
					...definition.statuses.map((status) => ({ value: status.value, label: status.label() }))
				]}
				onValueChange={(value) => setFilter('status', value)}
			/>
		{/if}

		{#if definition.filters.includes('inUse')}
			<SelectWithLabel
				id="resource-query-in-use"
				label={m.common_usage()}
				placeholder={m.common_all()}
				value={inputs.filters.value['inUse'] ?? ANY}
				options={[
					{ value: ANY, label: m.common_all() },
					{ value: 'true', label: m.common_in_use() },
					{ value: 'false', label: m.common_unused() }
				]}
				onValueChange={(value) => setFilter('inUse', value)}
			/>
		{/if}

		{#if definition.filters.includes('updates')}
			<SwitchWithLabel
				id="resource-query-updates"
				label={m.only_updates_available()}
				checked={inputs.filters.value['updates'] === 'has_update'}
				onCheckedChange={(checked) => setFilter('updates', checked ? 'has_update' : '')}
			/>
		{/if}
	{/if}

	<SelectWithLabel
		id="resource-query-display"
		label={m.display()}
		placeholder={m.count()}
		value={inputs.display.value ?? 'count'}
		options={[
			{ value: 'count', label: m.count() },
			{ value: 'list', label: m.list() }
		]}
		onValueChange={(value) => (inputs.display.value = value === 'list' ? 'list' : 'count')}
	/>

	<TextInputWithLabel
		id="resource-query-limit"
		type="number"
		label={m.common_limit()}
		placeholder={m.limit_placeholder({ default: DEFAULT_RESOURCE_LIMIT, max: MAX_RESOURCE_LIMIT })}
		min={1}
		value={inputs.limit.value ?? ''}
		error={inputs.limit.error}
		onChange={(value) => (inputs.limit.value = value === '' ? null : Number(value))}
	/>
</div>
