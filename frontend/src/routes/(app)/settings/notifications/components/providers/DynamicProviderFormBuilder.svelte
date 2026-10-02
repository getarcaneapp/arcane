<script lang="ts" generics="T extends object">
	import { Input } from '#lib/components/ui/input/index.js';
	import Textarea from '#lib/components/ui/textarea/textarea.svelte';
	import { Switch } from '#lib/components/ui/switch/index.js';
	import SelectWithLabel from '#lib/components/form/select-with-label.svelte';
	import SettingsRow from '#lib/components/settings/settings-row.svelte';
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

	function setSelectValue(
		field: ProviderNativeSelectField<T> | Extract<ProviderFormField<T>, { kind: 'select' }>,
		value: string
	): void {
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
		<SettingsRow for={getFieldId(field)} label={field.label} helpText={field.helpText} error={getFieldError(field)}>
			<Input
				id={getFieldId(field)}
				value={getInputValue(field.key)}
				oninput={(event) => setInputValue(field, (event.currentTarget as HTMLInputElement).value)}
				{disabled}
				placeholder={field.placeholder ?? ''}
				type={field.inputType ?? 'text'}
				autocomplete={field.autocomplete ?? 'off'}
				required={field.required ?? false}
				aria-invalid={!!getFieldError(field)}
			/>
		</SettingsRow>
	{:else if field.kind === 'textarea'}
		<SettingsRow for={getFieldId(field)} label={field.label} helpText={field.helpText} error={getFieldError(field)} layout="wide">
			<Textarea
				id={getFieldId(field)}
				value={getStringValue(field.key)}
				oninput={(event) => setStringValue(field.key, (event.target as HTMLTextAreaElement).value)}
				{disabled}
				autocomplete={field.autocomplete ?? 'off'}
				placeholder={field.placeholder ?? ''}
				rows={field.rows ?? 2}
				aria-invalid={!!getFieldError(field)}
			/>
		</SettingsRow>
	{:else if field.kind === 'switch'}
		<SettingsRow
			for={getFieldId(field)}
			label={field.label}
			description={field.description}
			error={getFieldError(field)}
			layout="switch"
		>
			<Switch
				id={getFieldId(field)}
				checked={getBooleanValue(field.key)}
				onCheckedChange={(value) => setBooleanValue(field.key, value)}
				{disabled}
			/>
		</SettingsRow>
	{:else if field.kind === 'select' || field.kind === 'native-select'}
		<SettingsRow for={getFieldId(field)} label={field.label} description={field.description} error={getFieldError(field)}>
			<SelectWithLabel
				id={getFieldId(field)}
				hideLabel
				value={getStringValue(field.key)}
				onValueChange={(value) => setSelectValue(field, value)}
				{disabled}
				label={field.label}
				placeholder={field.kind === 'select' ? field.placeholder : undefined}
				options={field.options.map((option) => ({ ...option, value: String(option.value) }))}
			/>
		</SettingsRow>
	{/if}
{/snippet}

{#each schema as node, index (`${index}-${node.kind}`)}
	{#if node.kind === 'row'}
		{#each node.fields as field, fieldIndex (`${fieldIndex}-${field.key}`)}
			{@render renderField(field)}
		{/each}
	{:else}
		{@render renderField(node)}
	{/if}
{/each}
