<script lang="ts">
	import { createQuery } from '@tanstack/svelte-query';

	import { Input } from '#lib/components/ui/input/index.js';
	import * as Select from '#lib/components/ui/select/index.js';
	import { m } from '#lib/paraglide/messages.js';
	import { projectListForEnvironmentQuery } from '#lib/query/projects.js';
	import { extractApiErrorMessage } from '#lib/utils/api.js';

	type Props = {
		environmentId: string;
		value: string;
		disabled?: boolean;
		onChange: (value: string) => void;
	};

	let { environmentId, value, disabled = false, onChange }: Props = $props();

	const projectsQuery = createQuery(() => ({
		...projectListForEnvironmentQuery(environmentId, {
			pagination: { page: 1, limit: -1 },
			sort: { column: 'name', direction: 'asc' },
			filters: { archived: 'all' }
		}),
		staleTime: 30_000
	}));

	const projects = $derived(projectsQuery.data?.data ?? []);
	const selectedLabel = $derived.by(() => {
		if (!value) return m.select_project();
		return projects.find((p) => p.id === value)?.name ?? value;
	});
</script>

{#if projects.length > 0}
	<Select.Root type="single" {value} {disabled} onValueChange={onChange}>
		<Select.Trigger class="w-full" aria-label={m.select_project()}>
			<span class="min-w-0 truncate">{selectedLabel}</span>
		</Select.Trigger>
		<Select.Content>
			{#each projects as proj (proj.id)}
				<Select.Item value={proj.id} label={proj.name}>
					{proj.name}
				</Select.Item>
			{/each}
		</Select.Content>
	</Select.Root>
{:else}
	<div class="space-y-1">
		<Input
			{value}
			{disabled}
			placeholder={m.resource_identifier()}
			aria-label={m.resource_identifier()}
			oninput={(e) => onChange(e.currentTarget.value.trim())}
		/>
		{#if projectsQuery.isError}
			<p class="text-xs text-destructive">{extractApiErrorMessage(projectsQuery.error)}</p>
		{/if}
	</div>
{/if}
