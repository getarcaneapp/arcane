<!-- Originally From  https://github.com/pocket-id/pocket-id/blob/main/frontend/src/lib/components/form/form-input.svelte -->
<script lang="ts">
	import { Input } from '$lib/components/ui/input';
	import DatePicker from '$lib/components/form/date-picker.svelte';
	import { Textarea } from '$lib/components/ui/textarea';
	import { Switch } from '$lib/components/ui/switch';
	import { Label } from '$lib/components/ui/label';
	import type { Snippet } from 'svelte';
	import type { HTMLAttributes } from 'svelte/elements';
	import type { FormInput } from '$lib/utils/form.utils';

	let {
		input = $bindable(),
		label,
		description,
		helpText,
		warningText,
		placeholder,
		disabled = false,
		type = 'text',
		rows = 3,
		children,
		autocomplete = 'off',
		...restProps
	}: HTMLAttributes<HTMLDivElement> & {
		input?: FormInput<string | boolean | number | Date | undefined>;
		label?: string;
		description?: string;
		helpText?: string;
		warningText?: string;
		placeholder?: string;
		disabled?: boolean;
		type?: 'text' | 'password' | 'email' | 'number' | 'checkbox' | 'date' | 'switch' | 'textarea';
		rows?: number;
		children?: Snippet;
		autocomplete?: HTMLInputElement['autocomplete'];
	} = $props();

	const id = $derived(label?.toLowerCase().replace(/ /g, '-'));
</script>

<div {...restProps}>
	{#if label}
		<Label class="mb-0" for={id}>{label}</Label>
	{/if}
	{#if description}
		<p class="text-muted-foreground mt-1 text-xs">{description}</p>
	{/if}
	<div class={label || description ? 'mt-2' : ''}>
		{#if children}
			{@render children()}
		{:else if input}
			{#if type === 'switch'}
				<Switch {id} bind:checked={input.value as boolean} {disabled} />
			{:else if type === 'textarea'}
				<Textarea {id} {placeholder} {rows} bind:value={input.value as string} {disabled} />
			{:else if type === 'date'}
				<DatePicker {id} bind:value={input.value as Date} />
			{:else}
				<Input {id} {placeholder} {type} bind:value={input.value} {disabled} {autocomplete} />
			{/if}
		{/if}
		{#if input?.error}
			<p class="mt-1 text-sm text-red-500">{input.error}</p>
		{/if}
		{#if helpText}
			<p class="text-muted-foreground mt-1 text-xs">{helpText}</p>
		{/if}
		{#if warningText}
			<p class="text-destructive mt-1 text-xs font-bold">{warningText}</p>
		{/if}
	</div>
</div>
