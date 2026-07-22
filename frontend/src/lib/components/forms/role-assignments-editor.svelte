<script lang="ts">
	import * as Select from '$lib/components/ui/select';
	import { Checkbox } from '$lib/components/ui/checkbox';
	import { ArcaneButton } from '$lib/components/arcane-button/index.js';
	import { Badge } from '$lib/components/ui/badge';
	import {
		type Role,
		BUILT_IN_ROLE_ADMIN,
		BUILT_IN_ROLE_EDITOR,
		BUILT_IN_ROLE_DEPLOYER,
		BUILT_IN_ROLE_VIEWER
	} from '$lib/types/auth';
	import type { Environment } from '$lib/types/environment';
	import { CloseIcon } from '$lib/icons';
	import { m } from '$lib/paraglide/messages';
	import { buildGlobalEnvironmentOptions, createRoleEnvironmentLabelers, GLOBAL_ENVIRONMENT_OPTION_ID } from '$lib/utils/options';

	type Assignment = { roleId: string; environmentId?: string };

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

	function envIdToSelectValue(envId: string | undefined): string {
		return envId ?? GLOBAL_ENVIRONMENT_OPTION_ID;
	}

	function selectValueToEnvId(value: string): string | undefined {
		return value === GLOBAL_ENVIRONMENT_OPTION_ID ? undefined : value;
	}

	function isEnvTaken(envValue: string, roleId: string, currentIndex: number): boolean {
		return assignments.some((a, i) => {
			if (i === currentIndex) return false;
			if (a.roleId !== roleId) return false;
			return envIdToSelectValue(a.environmentId) === envValue;
		});
	}

	function updateAssignment(index: number, patch: Partial<Assignment>) {
		if (disabled) return;
		assignments = assignments.map((a, i) => (i === index ? { ...a, ...patch } : a));
	}

	function addAssignment() {
		if (disabled) return;
		const defaultRoleId = roles[0]?.id ?? '';
		assignments = [...assignments, { roleId: defaultRoleId, environmentId: undefined }];
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
		const conflictingRoleId =
			roleId === BUILT_IN_ROLE_ADMIN ? BUILT_IN_ROLE_EDITOR : roleId === BUILT_IN_ROLE_EDITOR ? BUILT_IN_ROLE_ADMIN : '';
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
						<div class="flex items-center gap-2">
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

	{#each assignments as assignment, index (`${assignment.roleId}-${assignment.environmentId ?? 'global'}`)}
		{@const envValue = envIdToSelectValue(assignment.environmentId)}
		<div class="grid grid-cols-1 gap-2 rounded-md border bg-card/50 p-3 sm:grid-cols-[1fr_1fr_auto] sm:items-center">
			<Select.Root
				type="single"
				value={envValue}
				{disabled}
				onValueChange={(v) => updateAssignment(index, { environmentId: selectValueToEnvId(v) })}
			>
				<Select.Trigger class="w-full" aria-label={m.resource_environment_cap()}>
					<span>{selectedLabel.environment(envValue)}</span>
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
					<span class="flex items-center gap-2">
						<span>{selectedLabel.role(assignment.roleId)}</span>
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
	{/each}

	<ArcaneButton
		action="base"
		tone="outline"
		size="sm"
		type="button"
		onclick={addAssignment}
		{disabled}
		customLabel={m.users_role_assignments_add()}
	/>
</div>
