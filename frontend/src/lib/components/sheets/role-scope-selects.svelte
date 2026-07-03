<script lang="ts">
	import { Label } from '$lib/components/ui/label/index.js';
	import * as Select from '$lib/components/ui/select/index.js';

	interface RoleOption {
		id: string;
		name: string;
		description?: string | null;
	}

	interface EnvOption {
		id: string;
		name: string;
	}

	interface Props {
		idPrefix: string;
		roleLabel: string;
		scopeLabel: string;
		roles: RoleOption[];
		envOptions: EnvOption[];
		roleValue?: string;
		environmentValue?: string;
		roleError?: string | null;
		roleSelectedLabel: (value: string) => string;
		envSelectedLabel: (value: string) => string;
		disabled?: boolean;
		class?: string;
	}

	let {
		idPrefix,
		roleLabel,
		scopeLabel,
		roles,
		envOptions,
		roleValue = $bindable(''),
		environmentValue = $bindable(''),
		roleError,
		roleSelectedLabel,
		envSelectedLabel,
		disabled = false,
		class: className = 'grid gap-4'
	}: Props = $props();
</script>

<div class={className}>
	<div class="space-y-2">
		<Label for="{idPrefix}-role" class="mb-0">{roleLabel}</Label>
		<Select.Root type="single" bind:value={roleValue} {disabled}>
			<Select.Trigger id="{idPrefix}-role" class="w-full {roleError ? 'border-destructive' : ''}">
				<span>{roleSelectedLabel(roleValue)}</span>
			</Select.Trigger>
			<Select.Content>
				{#each roles as role (role.id)}
					<Select.Item value={role.id} label={role.name}>
						<div class="flex flex-col items-start gap-0.5">
							<span class="font-medium">{role.name}</span>
							{#if role.description}
								<span class="text-muted-foreground text-xs">{role.description}</span>
							{/if}
						</div>
					</Select.Item>
				{/each}
			</Select.Content>
		</Select.Root>
		{#if roleError}
			<p class="text-destructive text-xs font-medium">{roleError}</p>
		{/if}
	</div>
	<div class="space-y-2">
		<Label for="{idPrefix}-env" class="mb-0">{scopeLabel}</Label>
		<Select.Root type="single" bind:value={environmentValue} {disabled}>
			<Select.Trigger id="{idPrefix}-env" class="w-full">
				<span>{envSelectedLabel(environmentValue)}</span>
			</Select.Trigger>
			<Select.Content>
				{#each envOptions as option (option.id)}
					<Select.Item value={option.id} label={option.name}>
						{option.name}
					</Select.Item>
				{/each}
			</Select.Content>
		</Select.Root>
	</div>
</div>
