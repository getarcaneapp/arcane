<script lang="ts">
	import * as Select from '$lib/components/ui/select';
	import { ArcaneButton } from '$lib/components/arcane-button/index.js';
	import StatusBadge from '$lib/components/badges/status-badge.svelte';
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

	type Assignment = { roleId: string; environmentId?: string };

	type Props = {
		assignments: Assignment[];
		roles: Role[];
		environments: Environment[];
		disabled?: boolean;
	};

	let { assignments = $bindable([]), roles, environments, disabled = false }: Props = $props();

	const GLOBAL_OPTION_ID = 'global';

	type EnvOption = { id: string; name: string };

	const envOptions: EnvOption[] = $derived([
		{ id: GLOBAL_OPTION_ID, name: m.users_role_assignments_scope_global() },
		...environments.map((env) => ({ id: env.id, name: env.name }))
	]);

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
		return envId ?? GLOBAL_OPTION_ID;
	}

	function selectValueToEnvId(value: string): string | undefined {
		return value === GLOBAL_OPTION_ID ? undefined : value;
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

	function envSelectedLabel(value: string): string {
		return envOptions.find((o) => o.id === value)?.name ?? m.common_select_option();
	}

	function roleSelectedLabel(value: string): string {
		return roles.find((r) => r.id === value)?.name ?? m.common_select_option();
	}
</script>

<div class="space-y-3">
	{#if assignments.length === 0}
		<p class="text-muted-foreground rounded-md border border-dashed p-4 text-center text-sm">
			{m.users_role_assignments_description()}
		</p>
	{/if}

	{#each assignments as assignment, index (`${assignment.roleId}-${assignment.environmentId ?? 'global'}`)}
		{@const envValue = envIdToSelectValue(assignment.environmentId)}
		<div class="bg-card/50 grid grid-cols-1 gap-2 rounded-md border p-3 sm:grid-cols-[1fr_1fr_auto] sm:items-center">
			<Select.Root
				type="single"
				value={envValue}
				{disabled}
				onValueChange={(v) => updateAssignment(index, { environmentId: selectValueToEnvId(v) })}
			>
				<Select.Trigger class="w-full" aria-label={m.users_role_assignments_environment()}>
					<span>{envSelectedLabel(envValue)}</span>
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
				<Select.Trigger class="w-full" aria-label={m.users_role_assignments_role()}>
					<span class="flex items-center gap-2">
						<span>{roleSelectedLabel(assignment.roleId)}</span>
					</span>
				</Select.Trigger>
				<Select.Content>
					{#each roles as role (role.id)}
						<Select.Item value={role.id} label={role.name}>
							<span class="flex items-center gap-2">
								<StatusBadge text={role.name} variant={getRoleVariant(role.id)} size="sm" minWidth="none" />
								{#if role.description}
									<span class="text-muted-foreground text-xs">{role.description}</span>
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
				class="text-muted-foreground hover:text-destructive justify-self-end"
				customLabel={m.users_role_assignments_remove()}
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
