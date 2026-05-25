<script lang="ts">
	import * as Accordion from '$lib/components/ui/accordion';
	import { Checkbox } from '$lib/components/ui/checkbox';
	import { Input } from '$lib/components/ui/input';
	import StatusBadge from '$lib/components/badges/status-badge.svelte';
	import type { PermissionsManifest, PermissionResource, PermissionAction } from '$lib/types/auth';
	import { m } from '$lib/paraglide/messages';

	type Props = {
		manifest: PermissionsManifest;
		selected: string[];
		disabled?: boolean;
		showSearch?: boolean;
	};

	let { manifest, selected = $bindable([]), disabled = false, showSearch = true }: Props = $props();

	let query = $state('');

	const normalizedQuery = $derived(query.trim().toLowerCase());

	type FilteredResource = {
		resource: PermissionResource;
		actions: PermissionAction[];
	};

	const filteredGroups: FilteredResource[] = $derived(
		manifest.resources
			.map((resource) => {
				if (!normalizedQuery) {
					return { resource, actions: resource.actions };
				}
				const actions = resource.actions.filter(
					(action) =>
						action.label.toLowerCase().includes(normalizedQuery) || action.permission.toLowerCase().includes(normalizedQuery)
				);
				return { resource, actions };
			})
			.filter((g) => g.actions.length > 0)
	);

	const selectedSet = $derived(new Set(selected));

	const openValues: string[] = $derived(normalizedQuery ? filteredGroups.map((g) => g.resource.key) : []);

	function countSelectedInGroup(resource: PermissionResource): number {
		let count = 0;
		for (const action of resource.actions) {
			if (selectedSet.has(action.permission)) count++;
		}
		return count;
	}

	function groupCheckState(resource: PermissionResource): boolean | 'indeterminate' {
		const count = countSelectedInGroup(resource);
		if (count === 0) return false;
		if (count === resource.actions.length) return true;
		return 'indeterminate';
	}

	function toggleGroup(resource: PermissionResource, checked: boolean) {
		if (disabled) return;
		const groupPerms = resource.actions.map((a) => a.permission);
		if (checked) {
			const without = selected.filter((p) => !groupPerms.includes(p));
			selected = [...without, ...groupPerms];
		} else {
			selected = selected.filter((p) => !groupPerms.includes(p));
		}
	}

	function toggleAction(permission: string, checked: boolean) {
		if (disabled) return;
		if (checked) {
			if (!selected.includes(permission)) {
				selected = [...selected, permission];
			}
		} else {
			selected = selected.filter((p) => p !== permission);
		}
	}
</script>

<div class="space-y-4">
	{#if showSearch}
		<Input type="text" placeholder={m.permissions_search_placeholder()} bind:value={query} {disabled} />
	{/if}

	{#if filteredGroups.length === 0}
		<p class="text-muted-foreground py-6 text-center text-sm">{m.permissions_no_matches()}</p>
	{:else}
		<Accordion.Root type="multiple" value={openValues} class="w-full">
			{#each filteredGroups as group (group.resource.key)}
				{@const checkState = groupCheckState(group.resource)}
				{@const isAllChecked = checkState === true}
				{@const isIndeterminate = checkState === 'indeterminate'}
				<Accordion.Item value={group.resource.key}>
					<div class="flex w-full items-center gap-3 py-1">
						<Checkbox
							id={`group-${group.resource.key}`}
							checked={isAllChecked}
							indeterminate={isIndeterminate}
							{disabled}
							onCheckedChange={(checked) => toggleGroup(group.resource, checked === true)}
							aria-label={m.permissions_select_all()}
						/>
						<Accordion.Trigger class="flex-1 py-2 text-left text-sm font-medium">
							<div class="flex flex-1 items-center justify-between gap-2 pr-2">
								<span>
									{m.permissions_group_label({
										resource: group.resource.label,
										selected: countSelectedInGroup(group.resource),
										total: group.resource.actions.length
									})}
								</span>
								<StatusBadge
									text={group.resource.scope === 'global' ? m.permissions_scope_global() : m.permissions_scope_env()}
									variant={group.resource.scope === 'global' ? 'amber' : 'blue'}
									size="sm"
									minWidth="none"
								/>
							</div>
						</Accordion.Trigger>
					</div>
					<Accordion.Content>
						<div class="grid grid-cols-1 gap-3 pt-2 pl-7 sm:grid-cols-2">
							{#each group.actions as action (action.permission)}
								{@const checked = selectedSet.has(action.permission)}
								<label
									for={`perm-${action.permission}`}
									class="flex cursor-pointer items-start gap-3 rounded-md p-2 hover:bg-accent/40"
								>
									<Checkbox
										id={`perm-${action.permission}`}
										{checked}
										{disabled}
										onCheckedChange={(c) => toggleAction(action.permission, c === true)}
										class="mt-0.5"
									/>
									<div class="flex flex-col gap-0.5">
										<span class="text-sm leading-none font-medium">{action.label}</span>
										<code class="text-muted-foreground text-xs">{action.permission}</code>
										{#if action.description}
											<span class="text-muted-foreground text-xs">{action.description}</span>
										{/if}
									</div>
								</label>
							{/each}
						</div>
					</Accordion.Content>
				</Accordion.Item>
			{/each}
		</Accordion.Root>
	{/if}
</div>
