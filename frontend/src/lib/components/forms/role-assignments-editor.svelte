<script lang="ts">
	import { ArcaneButton } from '#lib/components/arcane-button/index.js';
	import { Badge } from '#lib/components/ui/badge/index.js';
	import { Checkbox } from '#lib/components/ui/checkbox/index.js';
	import { Input } from '#lib/components/ui/input/index.js';
	import * as Select from '#lib/components/ui/select/index.js';
	import { CloseIcon } from '#lib/icons/index.js';
	import { m } from '#lib/paraglide/messages.js';
	import type { Environment } from '#lib/types/environment.js';
	import {
		type ResourceScopeType,
		type Role,
		BUILT_IN_ROLE_ADMIN,
		BUILT_IN_ROLE_EDITOR,
		BUILT_IN_ROLE_DEPLOYER,
		BUILT_IN_ROLE_VIEWER
	} from '#lib/types/role.js';
	import {
		buildGlobalEnvironmentOptions,
		createRoleEnvironmentLabelers,
		GLOBAL_ENVIRONMENT_OPTION_ID
	} from '#lib/utils/options.js';

	type Assignment = { roleId: string; environmentId?: string; resourceType?: ResourceScopeType; resourceId?: string };

	const ALL_RESOURCES_OPTION = '__all__';

	const resourceTypeOptions: Array<{ value: string; label: () => string }> = [
		{ value: ALL_RESOURCES_OPTION, label: () => m.all_resources_in_environment() },
		{ value: 'project', label: () => m.project() },
		{ value: 'container', label: () => m.container() },
		{ value: 'volume', label: () => m.resource_volume_cap() },
		{ value: 'network', label: () => m.resource_network_cap() },
		{ value: 'image', label: () => m.common_image() },
		{ value: 'gitops', label: () => m.webhook_target_resource_label_gitops() }
	];

	function resourceTypeLabel(value: string | undefined): string {
		const found = resourceTypeOptions.find((opt) => opt.value === (value ?? ALL_RESOURCES_OPTION));
		return found ? found.label() : m.all_resources_in_environment();
	}

	type Props = {
		assignments: Assignment[];
		roles: Role[];
		environments: Environment[];
		disabled?: boolean;
	};

	let { assignments = $bindable([]), roles, environments, disabled = false }: Props = $props();

	const envOptions = $derived(buildGlobalEnvironmentOptions(environments, m.global_org_wide()));
	const selectedLabel = $derived(createRoleEnvironmentLabelers(roles, envOptions, m.common_select_option()));

	const quickPresetRoles = $derived(roles.filter((role) => role.id === BUILT_IN_ROLE_EDITOR || role.id === BUILT_IN_ROLE_ADMIN));

	function getRoleVariant(roleId: string) {
		switch (roleId) {
			case BUILT_IN_ROLE_ADMIN:
				return 'red';
			case BUILT_IN_ROLE_EDITOR:
				return 'blue';
			case BUILT_IN_ROLE_DEPLOYER:
				return 'purple';
			case BUILT_IN_ROLE_VIEWER:
				return 'gray';
			default:
				return 'green';
		}
	}

	function getConflictingRoleId(roleId: string): string {
		switch (roleId) {
			case BUILT_IN_ROLE_ADMIN:
				return BUILT_IN_ROLE_EDITOR;
			case BUILT_IN_ROLE_EDITOR:
				return BUILT_IN_ROLE_ADMIN;
			default:
				return '';
		}
	}

	function envIdToSelectValue(envId: string | undefined): string {
		return envId ?? GLOBAL_ENVIRONMENT_OPTION_ID;
	}

	function selectValueToEnvId(value: string): string | undefined {
		return value === GLOBAL_ENVIRONMENT_OPTION_ID ? undefined : value;
	}

	function isEnvTaken(envValue: string, roleId: string, currentIndex: number): boolean {
		const current = currentIndex >= 0 ? assignments[currentIndex] : undefined;
		if (current?.resourceType) return false;
		return assignments.some((a, i) => {
			if (i === currentIndex) return false;
			if (a.roleId !== roleId || a.resourceType) return false;
			return envIdToSelectValue(a.environmentId) === envValue;
		});
	}

	function updateAssignment(index: number, patch: Partial<Assignment>) {
		const row = assignments[index];
		if (disabled || !row) return;
		// Mutate in place so the row keeps its identity, which the {#each} key relies on.
		Object.assign(row, patch);
	}

	function addAssignment() {
		if (disabled) return;
		const defaultRoleId = roles[0]?.id ?? '';
		const freeEnv = envOptions.find((option) => !isEnvTaken(option.id, defaultRoleId, -1));
		const envValue = freeEnv?.id ?? GLOBAL_ENVIRONMENT_OPTION_ID;
		assignments = [...assignments, { roleId: defaultRoleId, environmentId: selectValueToEnvId(envValue) }];
	}

	function removeAssignment(index: number) {
		if (disabled) return;
		assignments = assignments.filter((_, i) => i !== index);
	}

	function hasGlobalRoleAssignment(roleId: string): boolean {
		return assignments.some((assignment) => assignment.roleId === roleId && !assignment.environmentId);
	}

	function toggleQuickPreset(roleId: string, checked: boolean) {
		if (disabled) return;
		const conflictingRoleId = getConflictingRoleId(roleId);
		let next = assignments;
		if (checked) {
			if (conflictingRoleId) {
				next = next.filter((assignment) => !(assignment.roleId === conflictingRoleId && !assignment.environmentId));
			}
			if (!next.some((assignment) => assignment.roleId === roleId && !assignment.environmentId)) {
				next = [...next, { roleId, environmentId: undefined }];
			}
		} else {
			next = next.filter((assignment) => !(assignment.roleId === roleId && !assignment.environmentId));
		}
		assignments = next;
	}
</script>

<div class="space-y-3">
	{#if quickPresetRoles.length > 0}
		<div class="space-y-2 rounded-md border p-3">
			{#each quickPresetRoles as role (role.id)}
				<label class="flex cursor-pointer items-start gap-3 rounded-md p-2 hover:bg-accent/40">
					<Checkbox
						checked={hasGlobalRoleAssignment(role.id)}
						{disabled}
						onCheckedChange={(checked) => toggleQuickPreset(role.id, checked === true)}
					/>
					<div class="flex flex-col gap-1">
						<div class="flex flex-wrap items-center gap-2">
							<Badge variant={getRoleVariant(role.id)} size="sm">{role.name}</Badge>
							<span class="text-xs text-muted-foreground">{m.global_org_wide()}</span>
						</div>
						{#if role.description}
							<span class="text-xs text-muted-foreground">{role.description}</span>
						{/if}
					</div>
				</label>
			{/each}
		</div>
	{/if}

	{#if assignments.length === 0}
		<p class="rounded-md border border-dashed p-4 text-center text-sm text-muted-foreground">
			{m.users_role_assignments_description()}
		</p>
	{/if}

	{#each assignments as assignment, index (assignment)}
		{@const envValue = envIdToSelectValue(assignment.environmentId)}
		{@const resourceTypeValue = assignment.resourceType ?? ALL_RESOURCES_OPTION}
		<div class="space-y-2 rounded-md border border-border/70 bg-card/10 p-3">
			<div class="grid grid-cols-1 gap-2 sm:grid-cols-halves-action sm:items-center">
				<Select.Root
					type="single"
					value={envValue}
					{disabled}
					onValueChange={(v) => {
						const nextEnvId = selectValueToEnvId(v);
						updateAssignment(
							index,
							nextEnvId
								? { environmentId: nextEnvId }
								: { environmentId: undefined, resourceType: undefined, resourceId: undefined }
						);
					}}
				>
					<Select.Trigger class="w-full" aria-label={m.resource_environment_cap()}>
						<span class="min-w-0 truncate">{selectedLabel.environment(envValue)}</span>
					</Select.Trigger>
					<Select.Content>
						{#each envOptions as option (option.id)}
							<Select.Item
								value={option.id}
								label={option.name}
								disabled={option.id !== envValue && isEnvTaken(option.id, assignment.roleId, index)}
							>
								{option.name}
							</Select.Item>
						{/each}
					</Select.Content>
				</Select.Root>

				<Select.Root
					type="single"
					value={assignment.roleId}
					{disabled}
					onValueChange={(v) => updateAssignment(index, { roleId: v })}
				>
					<Select.Trigger class="w-full" aria-label={m.common_role()}>
						<span class="flex min-w-0 items-center gap-2">
							<span class="truncate">{selectedLabel.role(assignment.roleId)}</span>
						</span>
					</Select.Trigger>
					<Select.Content>
						{#each roles as role (role.id)}
							<Select.Item value={role.id} label={role.name}>
								<span class="flex items-center gap-2">
									<Badge variant={getRoleVariant(role.id)} size="sm">{role.name}</Badge>
									{#if role.description}
										<span class="text-xs text-muted-foreground">{role.description}</span>
									{/if}
								</span>
							</Select.Item>
						{/each}
					</Select.Content>
				</Select.Root>

				<ArcaneButton
					action="remove"
					tone="ghost"
					size="icon"
					icon={CloseIcon}
					showLabel={false}
					onclick={() => removeAssignment(index)}
					{disabled}
					class="justify-self-end text-muted-foreground hover:text-destructive"
					customLabel={m.common_remove()}
				/>
			</div>

			{#if assignment.environmentId}
				<div class="grid grid-cols-1 gap-2 sm:grid-cols-2">
					<Select.Root
						type="single"
						value={resourceTypeValue}
						{disabled}
						onValueChange={(v) => {
							if (v === ALL_RESOURCES_OPTION) {
								updateAssignment(index, { resourceType: undefined, resourceId: undefined });
							} else {
								updateAssignment(index, { resourceType: v as ResourceScopeType, resourceId: assignment.resourceId ?? '' });
							}
						}}
					>
						<Select.Trigger class="w-full" aria-label={m.resource_scope()}>
							<span class="min-w-0 truncate">{resourceTypeLabel(assignment.resourceType)}</span>
						</Select.Trigger>
						<Select.Content>
							{#each resourceTypeOptions as option (option.value)}
								<Select.Item value={option.value} label={option.label()}>
									{option.label()}
								</Select.Item>
							{/each}
						</Select.Content>
					</Select.Root>

					{#if assignment.resourceType}
						<Input
							value={assignment.resourceId ?? ''}
							{disabled}
							placeholder={m.resource_identifier()}
							aria-label={m.resource_identifier()}
							oninput={(e) => updateAssignment(index, { resourceId: e.currentTarget.value.trim() })}
						/>
					{/if}
				</div>
			{/if}
		</div>
	{/each}

	<ArcaneButton
		action="base"
		tone="outline"
		type="button"
		onclick={addAssignment}
		{disabled}
		customLabel={m.users_role_assignments_add()}
	/>
</div>
