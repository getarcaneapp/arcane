<script lang="ts">
	import { createQuery, keepPreviousData, skipToken, useQueryClient } from '@tanstack/svelte-query';

	import FormInput from '#lib/components/form/form-input.svelte';
	import SearchableSelect from '#lib/components/form/searchable-select.svelte';
	import SelectWithLabel from '#lib/components/form/select-with-label.svelte';
	import { Label } from '#lib/components/ui/label/index.js';
	import { m } from '#lib/paraglide/messages.js';
	import { containerListQuery } from '#lib/query/containers.js';
	import { projectListQuery } from '#lib/query/projects.js';
	import { environmentStore, type EnvironmentRef } from '#lib/stores/environment.store.svelte.js';
	import type { DashboardResourceKind } from '#lib/types/auth.js';
	import { extractApiErrorMessage } from '#lib/utils/api.js';
	import { hasPermission } from '#lib/utils/auth.js';

	import { getContainerDisplayName } from '../../../containers/container-table.helpers';
	import { RESOURCE_KINDS } from './resource-kinds';
	import type { WidgetFormState } from './widget-sheet.svelte';

	type TargetKind = Extract<DashboardResourceKind, 'containers' | 'projects'>;
	type ResourceItem = { value: string; label: string };

	const PICKER_LIMIT = 50;

	let {
		form,
		kinds,
		titlePlaceholder
	}: {
		form: WidgetFormState;
		kinds: TargetKind[];
		titlePlaceholder: string;
	} = $props();

	const inputs = $derived(form.inputs);

	const queryClient = useQueryClient();

	let search = $state('');

	const kind = $derived(kinds.find((entry) => entry === inputs.resourceKind.value));
	const environments = $derived(
		environmentStore.available.filter(
			(environment) => environment.enabled && (!kind || hasPermission(RESOURCE_KINDS[kind].permission, environment.id))
		)
	);
	const environment = $derived(environments.find((entry) => entry.id === inputs.environmentIds.value[0]));

	async function loadResources(resourceKind: TargetKind, target: EnvironmentRef, term: string): Promise<ResourceItem[]> {
		const request = { search: term || undefined, pagination: { page: 1, limit: PICKER_LIMIT } };
		if (resourceKind === 'projects') {
			const page = await queryClient.fetchQuery(projectListQuery(target.id, [target], request));
			return page.data.map((project) => ({ value: project.id, label: project.name }));
		}
		const page = await queryClient.fetchQuery(containerListQuery(target.id, [target], request));
		return page.data.map((container) => ({ value: container.id, label: getContainerDisplayName(container) }));
	}

	const resourcesQuery = createQuery(() => {
		const resourceKind = kind;
		const target = environment;
		const term = search.trim();
		return {
			queryKey: [resourceKind, target?.id, 'dashboard', 'resource-picker', term],
			queryFn: resourceKind && target ? () => loadResources(resourceKind, target, term) : skipToken,
			placeholderData: keepPreviousData
		};
	});

	const items = $derived(resourcesQuery.data ?? []);

	function setEnvironment(environmentId: string) {
		if (environmentId === inputs.environmentIds.value[0]) return;
		search = '';
		inputs.environmentIds.value = [environmentId];
		inputs.resourceId.value = '';
	}

	function setKind(value: string) {
		const resourceKind = kinds.find((entry) => entry === value);
		if (!resourceKind || resourceKind === inputs.resourceKind.value) return;
		search = '';
		inputs.resourceKind.value = resourceKind;
		inputs.resourceId.value = '';
	}

	function setResource(resourceId: string) {
		search = '';
		inputs.resourceId.value = resourceId;
	}
</script>

<div class="grid gap-4">
	<FormInput label={m.title()} type="text" placeholder={titlePlaceholder} bind:input={inputs.title} />

	<SelectWithLabel
		id="widget-target-environment"
		label={m.resource_environment_cap()}
		placeholder={m.common_select_option()}
		value={inputs.environmentIds.value[0] ?? ''}
		error={inputs.environmentIds.error}
		options={environments.map((entry) => ({ value: entry.id, label: entry.name }))}
		onValueChange={setEnvironment}
	/>

	{#if kinds.length > 1}
		<SelectWithLabel
			id="widget-target-kind"
			label={m.resource_type()}
			placeholder={m.common_select_option()}
			value={inputs.resourceKind.value ?? ''}
			error={inputs.resourceKind.error}
			options={kinds.map((entry) => ({ value: entry, label: RESOURCE_KINDS[entry].label() }))}
			onValueChange={setKind}
		/>
	{/if}

	{#if kind}
		<div class="space-y-2">
			<Label for="widget-target-resource">{m.resource()}</Label>
			<SearchableSelect
				triggerId="widget-target-resource"
				class="w-full"
				size="default"
				{items}
				bind:value={() => inputs.resourceId.value, setResource}
				displayText={items.find((item) => item.value === inputs.resourceId.value)?.label ?? inputs.resourceId.value}
				oninput={(event) => (search = event.currentTarget.value)}
				isLoading={resourcesQuery.isFetching}
				disabled={!environment}
				selectText={m.common_select_option()}
				emptyText={resourcesQuery.error ? extractApiErrorMessage(resourcesQuery.error) : m.common_no_results_found()}
			/>
			{#if inputs.resourceId.error}
				<p class="text-xs font-medium text-destructive">{inputs.resourceId.error}</p>
			{/if}
		</div>
	{/if}
</div>
