<script lang="ts" generics="T extends object">
	import { Label } from '$lib/components/ui/label';
	import Textarea from '$lib/components/ui/textarea/textarea.svelte';
	import SelectWithLabel from '$lib/components/form/select-with-label.svelte';
	import SwitchWithLabel from '$lib/components/form/labeled-switch.svelte';
	import TextInputWithLabel from '$lib/components/form/text-input-with-label.svelte';
	import type {
		ProviderFieldKey,
		ProviderFormField,
		ProviderFormSchema,
		ProviderNativeSelectField
	} from './provider-form-schema';

	interface Props {
		values: T;
		schema: ProviderFormSchema<T>;
		errors?: Partial<Record<keyof T, string>>;
		disabled?: boolean;
	}

	let { values = $bindable(), schema, errors = {}, disabled = false }: Props = $props();

	function getFieldId(field: ProviderFormField<T>): string {
		return field.id ?? String(field.key);
	}

	function getFieldError(field: ProviderFormField<T>): string | undefined {
		const key = (field.errorKey ?? field.key) as keyof T;
		return errors[key];
	}

	function getInputValue(key: ProviderFieldKey<T>): string | number {
		const value = values[key];
		if (typeof value === 'number') {
			return value;
		}

		return String(value ?? '');
	}

	function getStringValue(key: ProviderFieldKey<T>): string {
		return String(values[key] ?? '');
	}

	function getBooleanValue(key: ProviderFieldKey<T>): boolean {
		return Boolean(values[key]);
	}

	function setValue(key: ProviderFieldKey<T>, value: unknown): void {
		(values as Record<string, unknown>)[key] = value;
	}

	function setInputValue(field: Extract<ProviderFormField<T>, { kind: 'input' }>, value: string): void {
		if (field.inputType === 'number') {
			setValue(field.key, value === '' ? '' : Number(value));
			return;
		}

		setValue(field.key, value);
	}

	function setStringValue(key: ProviderFieldKey<T>, value: string): void {
		setValue(key, value);
	}

	function setBooleanValue(key: ProviderFieldKey<T>, value: boolean): void {
		setValue(key, value);
	}

	function setNativeSelectValue(field: ProviderNativeSelectField<T>, value: string): void {
		const currentValue = values[field.key];
		if (field.valueType === 'number' || typeof currentValue === 'number') {
			setValue(field.key, Number(value));
			return;
		}

		setValue(field.key, value);
	}

	function setSelectValue(field: Extract<ProviderFormField<T>, { kind: 'select' }>, value: string): void {
		const currentValue = values[field.key];
		if (field.valueType === 'number' || typeof currentValue === 'number') {
			setValue(field.key, Number(value));
			return;
		}

		setValue(field.key, value);
	}
</script>

{#snippet renderField(field: ProviderFormField<T>)}
	{#if field.kind === 'input'}
		<TextInputWithLabel
			id={getFieldId(field)}
			value={getInputValue(field.key)}
			onChange={(value) => setInputValue(field, value)}
			{disabled}
			label={field.label}
			placeholder={field.placeholder ?? ''}
			type={field.inputType ?? 'text'}
			autocomplete={field.autocomplete ?? 'off'}
			helpText={field.helpText}
			error={getFieldError(field)}
			required={field.required ?? false}
		/>
	{:else if field.kind === 'textarea'}
		<div class="space-y-2">
			<Label for={getFieldId(field)}>{field.label}</Label>
			<Textarea
				id={getFieldId(field)}
				value={getStringValue(field.key)}
				oninput={(event) => setStringValue(field.key, (event.target as HTMLTextAreaElement).value)}
				{disabled}
				autocomplete={field.autocomplete ?? 'off'}
				placeholder={field.placeholder ?? ''}
				rows={field.rows ?? 2}
			/>
			{#if getFieldError(field)}
				<p class="text-destructive text-sm">{getFieldError(field)}</p>
			{:else if field.helpText}
				<p class="text-muted-foreground text-sm">{field.helpText}</p>
			{/if}
		</div>
	{:else if field.kind === 'switch'}
		<SwitchWithLabel
			id={getFieldId(field)}
			checked={getBooleanValue(field.key)}
			onCheckedChange={(value) => setBooleanValue(field.key, value)}
			{disabled}
			label={field.label}
			description={field.description}
			error={getFieldError(field)}
		/>
	{:else if field.kind === 'select'}
		<SelectWithLabel
			id={getFieldId(field)}
			value={getStringValue(field.key)}
			onValueChange={(value) => setSelectValue(field, value)}
			{disabled}
			label={field.label}
			placeholder={field.placeholder}
			description={field.description}
			error={getFieldError(field)}
			options={field.options}
		/>
	{:else if field.kind === 'native-select'}
		<div class="space-y-2">
			<Label for={getFieldId(field)}>{field.label}</Label>
			<select
				id={getFieldId(field)}
				value={getStringValue(field.key)}
				onchange={(event) => setNativeSelectValue(field, (event.target as HTMLSelectElement).value)}
				{disabled}
				class="border-input bg-background ring-offset-background placeholder:text-muted-foreground focus-visible:ring-ring flex h-10 rounded-md border px-3 py-2 text-base focus-visible:ring-2 focus-visible:ring-offset-2 focus-visible:outline-none disabled:cursor-not-allowed disabled:opacity-50 md:text-sm"
			>
				{#each field.options as option (option.value)}
					<option value={option.value}>{option.label}</option>
				{/each}
			</select>
			{#if getFieldError(field)}
				<p class="text-destructive text-sm">{getFieldError(field)}</p>
			{:else if field.description}
				<p class="text-muted-foreground text-sm">{field.description}</p>
			{/if}
		</div>
	{/if}
{/snippet}

{#each schema as node, index (`${index}-${node.kind}`)}
	{#if node.kind === 'row'}
		<div class={node.className ?? 'grid grid-cols-2 gap-4'}>
			{#each node.fields as field, fieldIndex (`${fieldIndex}-${field.key}`)}
				<div class={field.wrapperClass ?? ''}>
					{@render renderField(field)}
				</div>
			{/each}
		</div>
	{:else if node.wrapperClass}
		<div class={node.wrapperClass}>
			{@render renderField(node)}
		</div>
	{:else}
		{@render renderField(node)}
	{/if}
{/each}
