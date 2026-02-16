<script lang="ts">
	import { Label } from '$lib/components/ui/label';
	import { Input } from '$lib/components/ui/input';

	let {
		id,
		name,
		value = $bindable(''),
		label,
		placeholder = '',
		description,
		helpText,
		reserveHelpTextSpace = false,
		error,
		disabled = false,
		type = 'text',
		autocomplete = 'off',
		required = false,
		onChange
	}: {
		id?: string;
		name?: string;
		value: string | number;
		label: string;
		placeholder?: string;
		description?: string;
		helpText?: string;
		reserveHelpTextSpace?: boolean;
		error?: string | null;
		disabled?: boolean;
		type?: 'text' | 'email' | 'password' | 'number' | 'url';
		autocomplete?: HTMLInputElement['autocomplete'];
		required?: boolean;
		onChange?: (value: string) => void;
	} = $props();

	function handleInput(e: Event) {
		const target = e.target as HTMLInputElement;
		if (type === 'number') {
			const numValue = target.value === '' ? '' : Number(target.value);
			value = numValue;
			onChange?.(target.value);
		} else {
			value = target.value;
			onChange?.(target.value);
		}
	}
</script>

<div class="grid gap-2">
	<Label for={id} class="text-sm leading-none font-medium">
		{label}{#if required}<span class="text-destructive ml-0.5">*</span>{/if}
	</Label>

	<Input
		{id}
		{name}
		bind:value
		{placeholder}
		{disabled}
		{type}
		{autocomplete}
		{required}
		oninput={handleInput}
		class={error ? 'border-destructive' : ''}
	/>

	{#if error}
		<p class="text-destructive text-[0.8rem] font-medium">{error}</p>
	{/if}
	{#if description}
		<p class="text-muted-foreground text-[0.8rem]">{description}</p>
	{/if}
	{#if helpText || reserveHelpTextSpace}
		<p class="text-muted-foreground min-h-[1rem] text-[0.7rem]">{helpText}</p>
	{/if}
</div>
