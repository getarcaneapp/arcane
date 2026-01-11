<script lang="ts">
	import * as DropdownMenu from '$lib/components/ui/dropdown-menu/index.js';
	import * as ButtonGroup from '$lib/components/ui/button-group/index.js';
	import * as InputGroup from '$lib/components/ui/input-group/index.js';
	import { Label } from '$lib/components/ui/label/index.js';
	import { ArcaneButton } from '$lib/components/arcane-button/index.js';
	import { ArrowDownIcon } from '$lib/icons';

	type Props = {
		id?: string;
		label?: string;
		description?: string;
		placeholder?: string;
		value?: string;
		protocol?: 'https' | 'http';
		disabled?: boolean;
		required?: boolean;
		error?: string;
	};

	let {
		id = 'url-input',
		label,
		description,
		placeholder = 'example.com',
		value = $bindable(''),
		protocol = $bindable('https'),
		disabled = false,
		required = false,
		error
	}: Props = $props();

	function selectProtocol(p: 'https' | 'http') {
		protocol = p;
	}
</script>

<div class="grid gap-2">
	{#if label}
		<Label for={id} class="text-xs">{label}</Label>
	{/if}

	<ButtonGroup.Root class="w-full">
		<DropdownMenu.Root>
			<DropdownMenu.Trigger>
				{#snippet child({ props })}
					<ArcaneButton
						{...props}
						action="base"
						tone="outline"
						class="w-[5.5rem] shrink-0 justify-between gap-1 text-sm"
						{disabled}
						customLabel="{protocol}://"
					>
						<ArrowDownIcon />
					</ArcaneButton>
				{/snippet}
			</DropdownMenu.Trigger>
			<DropdownMenu.Content align="center" class="min-w-0">
				<DropdownMenu.Item class="py-1.5" onclick={() => selectProtocol('https')}>https://</DropdownMenu.Item>
				<DropdownMenu.Item class="py-1.5" onclick={() => selectProtocol('http')}>http://</DropdownMenu.Item>
			</DropdownMenu.Content>
		</DropdownMenu.Root>
		<InputGroup.Root class={['flex-1', error ? 'border-destructive' : ''].filter(Boolean).join(' ')}>
			<InputGroup.Input {id} bind:value {placeholder} {disabled} {required} aria-invalid={!!error} />
		</InputGroup.Root>
	</ButtonGroup.Root>

	{#if description && !error}
		<p class="text-muted-foreground text-xs">{description}</p>
	{/if}

	{#if error}
		<p class="text-destructive text-xs">{error}</p>
	{/if}
</div>
