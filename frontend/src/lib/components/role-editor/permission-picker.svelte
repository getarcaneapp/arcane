<script lang="ts">
	import * as Accordion from '$lib/components/ui/accordion';
	import { Checkbox } from '$lib/components/ui/checkbox';
	import { Input } from '$lib/components/ui/input';
	import { Badge } from '$lib/components/ui/badge';
	import type { PermissionsManifest, PermissionResource, PermissionAction, PermissionPreset } from '$lib/types/auth';
	import { normalizePermissionSelection } from '$lib/utils/permissions';
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
	const presets = $derived(manifest.presets ?? []);

	const openValues: string[] = $derived(normalizedQuery ? filteredGroups.map((g) => g.resource.key) : []);

	function replaceSelected(next: string[]) {
		selected = normalizePermissionSelection(manifest, next);
	}

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
			replaceSelected([...without, ...groupPerms]);
		} else {
			replaceSelected(selected.filter((p) => !groupPerms.includes(p)));
		}
	}

	function toggleAction(permission: string, checked: boolean) {
		if (disabled) return;
		if (checked) {
			if (!selected.includes(permission)) {
				replaceSelected([...selected, permission]);
			}
		} else {
			replaceSelected(selected.filter((p) => p !== permission));
		}
	}

	function isPresetSelected(preset: PermissionPreset): boolean {
		return preset.permissions.length > 0 && preset.permissions.every((permission) => selectedSet.has(permission));
	}

	function togglePreset(preset: PermissionPreset, checked: boolean) {
		if (disabled) return;
		if (checked) {
			replaceSelected([...selected, ...preset.permissions]);
		} else {
			replaceSelected(selected.filter((permission) => !preset.permissions.includes(permission)));
		}
	}
</script>

<div class="space-y-4">
	{#if presets.length > 0}
		<div class="space-y-2 rounded-md border p-3">
			{#each presets as preset (preset.key)}
				<label class="flex cursor-pointer items-start gap-3 rounded-md p-2 hover:bg-accent/40">
					<Checkbox
						checked={isPresetSelected(preset)}
						{disabled}
						onCheckedChange={(checked) => togglePreset(preset, checked === true)}
					/>
					<div class="flex flex-col gap-0.5">
						<span class="text-sm leading-none font-medium">{preset.label}</span>
						{#if preset.description}
							<span class="text-xs text-muted-foreground">{preset.description}</span>
						{/if}
					</div>
				</label>
			{/each}
		</div>
	{/if}

	{#if showSearch}
		<Input type="text" placeholder={m.permissions_search_placeholder()} bind:value={query} {disabled} />
	{/if}

	{#if filteredGroups.length === 0}
		<p class="py-6 text-center text-sm text-muted-foreground">{m.permissions_no_matches()}</p>
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
							aria-label={m.common_select_all()}
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
								<Badge variant={group.resource.scope === 'global' ? 'amber' : 'blue'} size="sm"
									>{group.resource.scope === 'global' ? m.global() : m.permissions_scope_env()}</Badge
								>
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
										<code class="text-xs text-muted-foreground">{action.permission}</code>
										{#if action.description}
											<span class="text-xs text-muted-foreground">{action.description}</span>
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
