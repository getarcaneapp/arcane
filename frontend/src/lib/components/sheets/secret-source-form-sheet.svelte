<script lang="ts">
	import { createQuery } from '@tanstack/svelte-query';
	import { untrack } from 'svelte';

	import { ArcaneButton } from '#lib/components/arcane-button/index.js';
	import SwitchWithLabel from '#lib/components/form/labeled-switch.svelte';
	import SelectWithLabel from '#lib/components/form/select-with-label.svelte';
	import EnvironmentScopeSelect from '#lib/components/sheets/environment-scope-select.svelte';
	import SheetFooterActions from '#lib/components/sheets/sheet-footer-actions.svelte';
	import { Badge } from '#lib/components/ui/badge/index.js';
	import { Input } from '#lib/components/ui/input/index.js';
	import { Label } from '#lib/components/ui/label/index.js';
	import * as ResponsiveDialog from '#lib/components/ui/responsive-dialog/index.js';
	import { Textarea } from '#lib/components/ui/textarea/index.js';
	import * as m from '#lib/paraglide/messages.js';
	import { secretSourcePresetsQuery } from '#lib/query/secret-sources.js';
	import { secretSourceService } from '#lib/services/secret-source-service.js';
	import type { Environment } from '#lib/types/environment.js';
	import type {
		SecretSource,
		SecretSourceDefinition,
		SecretSourceField,
		SecretSourceInput,
		SecretSourceTestResult,
		SecretSourceUpdate
	} from '#lib/types/secretsource.js';
	import { extractApiErrorMessage } from '#lib/utils/api.js';
	import { changedFields } from '#lib/utils/payload.js';
	import { preventDefault } from '#lib/utils/settings.svelte.js';
	import { tryCatch } from '#lib/utils/try-catch.js';

	type SecretSourceFormPayload =
		| { mode: 'create'; source: SecretSourceInput }
		| { mode: 'edit'; id: string; source: SecretSourceUpdate };

	const CUSTOM = '__custom';

	let {
		open = $bindable(false),
		sourceToEdit = null,
		environments,
		isLoading = false,
		onSubmit
	}: {
		open: boolean;
		sourceToEdit: SecretSource | null;
		environments: Environment[];
		isLoading?: boolean;
		onSubmit: (payload: SecretSourceFormPayload) => Promise<Error | null>;
	} = $props();

	const isEditMode = $derived(!!sourceToEdit);

	const presetsQuery = createQuery(() => secretSourcePresetsQuery());
	const presets = $derived(presetsQuery.data ?? []);

	let name = $state(untrack(() => sourceToEdit?.name ?? ''));
	let pickedChoice = $state('');
	let customYaml = $state('');
	let customDefinition = $state<SecretSourceDefinition | null>(null);
	let definitionError = $state<string | null>(null);
	let validating = $state(false);
	let validationRun = 0;
	let inputValues = $state<Record<string, string>>(untrack(() => ({ ...sourceToEdit?.inputs })));
	let credentialValues = $state<Record<string, string>>({});
	let clearedCredentials = $state<Record<string, boolean>>({});
	let path = $state(untrack(() => sourceToEdit?.path ?? ''));
	let keyPrefix = $state(untrack(() => sourceToEdit?.keyPrefix ?? ''));
	let enabled = $state(untrack(() => sourceToEdit?.enabled ?? true));
	let injectIntoContainers = $state(untrack(() => sourceToEdit?.injectIntoContainers ?? false));
	let scope = $state<'all' | 'specific'>(
		untrack(() => {
			if (sourceToEdit && !sourceToEdit.allEnvironments && sourceToEdit.environmentIds.length > 0) return 'specific';
			return 'all';
		})
	);
	let selectedEnvIds = $state<string[]>(untrack(() => [...(sourceToEdit?.environmentIds ?? [])]));
	let scopeError = $state<string | null>(null);
	let nameError = $state<string | null>(null);
	let pathError = $state<string | null>(null);
	let fieldErrors = $state<Record<string, string>>({});
	let testing = $state(false);
	let testResult = $state<SecretSourceTestResult | null>(null);
	let testError = $state<string | null>(null);

	const choice = $derived.by(() => {
		if (pickedChoice) return pickedChoice;
		if (sourceToEdit) return sourceToEdit.presetId || CUSTOM;
		return presets[0]?.definition.id ?? CUSTOM;
	});

	const choiceOptions = $derived([
		...presets.map((preset) => ({
			value: preset.definition.id,
			label: preset.definition.name,
			description: preset.definition.description,
			badge: preset.definition.kind
		})),
		{ value: CUSTOM, label: m.custom_definition(), description: m.custom_definition_description() }
	]);

	const activeDefinition = $derived.by(() => {
		if (choice === CUSTOM) {
			if (customDefinition) return customDefinition;
			return sourceToEdit && !sourceToEdit.presetId && customYaml.trim() === '' ? sourceToEdit.definition : null;
		}
		const preset = presets.find((entry) => entry.definition.id === choice);
		if (preset) return preset.definition;
		return sourceToEdit?.presetId === choice ? sourceToEdit.definition : null;
	});

	function inputChanged(key: string): boolean {
		return (inputValues[key] ?? '').trim() !== (sourceToEdit?.inputs?.[key] ?? '');
	}

	const dropsSavedCredentials = $derived.by(() => {
		if (!sourceToEdit) return false;
		if (choice !== (sourceToEdit.presetId || CUSTOM)) return true;
		if (choice === CUSTOM && customYaml.trim() !== '') return true;
		if (inputChanged('baseUrl')) return true;
		return activeDefinition?.kind === 'container' && activeDefinition.inputs.some((field) => inputChanged(field.key));
	});

	function credentialIsSaved(field: SecretSourceField): boolean {
		return !!sourceToEdit && sourceToEdit.credentialKeys.includes(field.key);
	}

	function credentialIsSet(field: SecretSourceField): boolean {
		return credentialIsSaved(field) && !dropsSavedCredentials;
	}

	function resetTest() {
		testResult = null;
		testError = null;
	}

	async function validateCustomDefinition(): Promise<SecretSourceDefinition | null> {
		const run = ++validationRun;
		if (customYaml.trim() === '') {
			validating = false;
			customDefinition = null;
			definitionError = null;
			return null;
		}
		validating = true;
		const result = await tryCatch(secretSourceService.validateDefinition(customYaml));
		if (run !== validationRun) return null;
		validating = false;
		if (result.error) {
			customDefinition = null;
			definitionError = extractApiErrorMessage(result.error);
			return null;
		}
		customDefinition = result.data;
		definitionError = null;
		return result.data;
	}

	function handleChoiceChange(value: string) {
		const previous = presets.find((entry) => entry.definition.id === choice);
		pickedChoice = value;
		fieldErrors = {};
		definitionError = null;
		resetTest();
		if (value === CUSTOM && customYaml.trim() === '' && previous) {
			customYaml = previous.source;
			void validateCustomDefinition();
		}
	}

	function handleYamlInput() {
		validationRun += 1;
		validating = false;
		customDefinition = null;
		definitionError = null;
		resetTest();
	}

	function scopeDto(): Pick<SecretSourceInput, 'allEnvironments' | 'environmentIds'> {
		if (scope === 'all') return { allEnvironments: true, environmentIds: [] };
		return { allEnvironments: false, environmentIds: [...selectedEnvIds] };
	}

	function buildInput(definition: SecretSourceDefinition): SecretSourceInput {
		return {
			name: name.trim(),
			presetId: choice === CUSTOM ? '' : choice,
			definition: choice === CUSTOM ? customYaml : '',
			inputs: Object.fromEntries(definition.inputs.map((field) => [field.key, (inputValues[field.key] ?? '').trim()])),
			credentials: Object.fromEntries(
				definition.credentials
					.filter((field) => (credentialValues[field.key] ?? '') !== '' || clearedCredentials[field.key])
					.map((field) => [field.key, clearedCredentials[field.key] ? '' : (credentialValues[field.key] ?? '')])
			),
			path: path.trim(),
			keyPrefix: keyPrefix.trim(),
			injectIntoContainers,
			enabled,
			...scopeDto()
		};
	}

	async function resolveDefinition(): Promise<SecretSourceDefinition | null> {
		if (choice === CUSTOM && customYaml.trim() !== '' && !customDefinition) {
			return validateCustomDefinition();
		}
		if (!activeDefinition) {
			definitionError = m.common_field_required({ field: m.definition() });
		}
		return activeDefinition;
	}

	function validateFields(definition: SecretSourceDefinition): boolean {
		const errors: Record<string, string> = {};
		for (const field of definition.inputs) {
			if (field.required && !field.default && (inputValues[field.key] ?? '').trim() === '') {
				errors[`input:${field.key}`] = m.common_field_required({ field: field.label });
			}
		}
		for (const field of definition.credentials) {
			const empty = (credentialValues[field.key] ?? '') === '';
			if (field.required && !field.default && !credentialIsSet(field) && empty) {
				errors[`credential:${field.key}`] = m.common_field_required({ field: field.label });
			} else if (dropsSavedCredentials && credentialIsSaved(field) && empty && !clearedCredentials[field.key]) {
				errors[`credential:${field.key}`] = m.enter_saved_value_again_or_clear();
			}
		}
		fieldErrors = errors;
		return Object.keys(errors).length === 0;
	}

	function validateScope(): boolean {
		scopeError = scope === 'specific' && selectedEnvIds.length === 0 ? m.select_at_least_one_environment() : null;
		return !scopeError;
	}

	async function handleTest() {
		resetTest();
		const definition = await resolveDefinition();
		if (!definition) return;
		testing = true;
		const result = await tryCatch(secretSourceService.test({ ...buildInput(definition), id: sourceToEdit?.id }));
		testing = false;
		if (result.error) {
			testError = extractApiErrorMessage(result.error);
			return;
		}
		if (!result.data.success) {
			testError = result.data.error ?? m.common_test_failed({ resource: m.external_secret() });
			return;
		}
		testResult = result.data;
	}

	async function handleSubmit() {
		nameError = name.trim() === '' ? m.common_field_required({ field: m.common_name() }) : null;
		const scopeValid = validateScope();
		const definition = await resolveDefinition();
		if (!definition) return;
		const fieldsValid = validateFields(definition);
		pathError = definition.pathRequired && path.trim() === '' ? m.common_field_required({ field: m.path() }) : null;
		if (nameError || pathError || !scopeValid || !fieldsValid) return;

		const input = buildInput(definition);
		if (!sourceToEdit) {
			await onSubmit({ mode: 'create', source: input });
			return;
		}

		const { definition: yaml, credentials, presetId, ...rest } = input;
		const source: SecretSourceUpdate = changedFields(sourceToEdit, rest);
		if (presetId !== (sourceToEdit.presetId ?? '')) source.presetId = presetId;
		if (yaml.trim() !== '') source.definition = yaml;
		if (Object.keys(credentials).length > 0) source.credentials = credentials;
		await onSubmit({ mode: 'edit', id: sourceToEdit.id, source });
	}
</script>

<ResponsiveDialog.Root
	bind:open
	variant="sheet"
	title={isEditMode ? m.edit_external_secret() : m.create_external_secret()}
	description={isEditMode ? (sourceToEdit?.name ?? '') : m.external_secret_form_description()}
	contentClass="sm:max-w-135"
>
	<form onsubmit={preventDefault(handleSubmit)} class="grid gap-4 py-6">
		<div>
			<Label for="secret-source-name">{m.common_name()}</Label>
			<Input id="secret-source-name" type="text" class="mt-2" placeholder={m.common_name_placeholder()} bind:value={name} />
			{#if nameError}
				<p class="mt-1 text-sm text-destructive">{nameError}</p>
			{/if}
		</div>

		<SelectWithLabel
			id="secret-source-preset"
			label={m.preset()}
			description={m.preset_description()}
			value={choice}
			options={choiceOptions}
			disabled={presetsQuery.isPending}
			placeholder={presetsQuery.isPending ? m.common_loading() : m.common_select_option()}
			onValueChange={handleChoiceChange}
		/>
		{#if presetsQuery.error}
			<p class="text-sm text-destructive">{extractApiErrorMessage(presetsQuery.error)}</p>
		{/if}

		{#if choice === CUSTOM}
			<div>
				<Label for="secret-source-definition">{m.definition()}</Label>
				<Textarea
					id="secret-source-definition"
					rows={12}
					mono
					class="mt-2"
					placeholder={isEditMode ? m.definition_keep_placeholder() : m.definition_placeholder()}
					bind:value={customYaml}
					oninput={handleYamlInput}
					onblur={() => void validateCustomDefinition()}
				/>
				{#if validating}
					<p class="mt-1 text-xs text-muted-foreground">{m.common_loading()}</p>
				{:else if definitionError}
					<p class="mt-1 text-sm whitespace-pre-wrap text-destructive">{definitionError}</p>
				{/if}
			</div>
		{:else if definitionError}
			<p class="text-sm text-destructive">{definitionError}</p>
		{/if}

		{#if activeDefinition}
			{#each activeDefinition.inputs as field (field.key)}
				{@render definitionField(field, 'input')}
			{/each}
			{#each activeDefinition.credentials as field (field.key)}
				{@render definitionField(field, 'credential')}
			{/each}
		{/if}

		<div>
			<Label for="secret-source-path">
				{m.path()}{#if activeDefinition?.pathRequired}<span class="ml-0.5 text-destructive">*</span>{/if}
			</Label>
			<Input id="secret-source-path" type="text" mono class="mt-2" placeholder={m.path_placeholder()} bind:value={path} />
			<p class="mt-1 text-xs text-muted-foreground">{m.path_description()}</p>
			{#if pathError}
				<p class="mt-1 text-sm text-destructive">{pathError}</p>
			{/if}
		</div>

		<div>
			<Label for="secret-source-key-prefix">{m.key_prefix()}</Label>
			<Input
				id="secret-source-key-prefix"
				type="text"
				mono
				class="mt-2"
				placeholder={m.key_prefix_placeholder()}
				bind:value={keyPrefix}
			/>
			<p class="mt-1 text-xs text-muted-foreground">{m.key_prefix_description()}</p>
		</div>

		<SwitchWithLabel
			id="secret-source-enabled"
			label={m.common_enabled()}
			description={m.external_secret_enabled_description()}
			bind:checked={enabled}
		/>

		<SwitchWithLabel
			id="secret-source-inject"
			label={m.inject_into_all_containers()}
			description={m.inject_into_all_containers_description()}
			bind:checked={injectIntoContainers}
		/>

		<EnvironmentScopeSelect {environments} bind:scope bind:selected={selectedEnvIds} bind:error={scopeError} />

		<div class="grid gap-2">
			<ArcaneButton action="test" tone="outline" type="button" class="w-fit" loading={testing} onclick={handleTest} />
			{#if testError}
				<p class="text-sm whitespace-pre-wrap text-destructive">{testError}</p>
			{:else if testResult}
				<div class="rounded-md border border-border/50">
					<p class="px-3 py-2 text-sm font-medium">{m.count_keys_found({ count: testResult.keyCount })}</p>
					{#if testResult.keys.length > 0}
						<div class="flex max-h-48 flex-wrap gap-1 overflow-y-auto border-t border-border/50 px-3 py-2">
							{#each testResult.keys as key (key)}
								<Badge variant="outline" size="sm"><span class="font-mono">{key}</span></Badge>
							{/each}
						</div>
					{/if}
					{#if testResult.skippedKeyCount > 0}
						<p class="border-t border-border/50 px-3 py-2 text-xs text-muted-foreground">
							{m.count_keys_skipped({ count: testResult.skippedKeyCount })}
						</p>
					{/if}
				</div>
			{/if}
		</div>
	</form>

	{#snippet footer()}
		<SheetFooterActions
			bind:open
			cancelDisabled={isLoading}
			submitAction={isEditMode ? 'save' : 'create'}
			submitDisabled={isLoading || validating}
			submitLoading={isLoading}
			onSubmit={handleSubmit}
			submitLabel={isEditMode ? m.common_save_changes() : m.common_add_button({ resource: m.external_secret() })}
		/>
	{/snippet}
</ResponsiveDialog.Root>

{#snippet definitionField(field: SecretSourceField, kind: 'input' | 'credential')}
	{@const id = `secret-source-${kind}-${field.key}`}
	{@const error = fieldErrors[`${kind}:${field.key}`]}
	<div>
		<Label for={id}>{field.label}</Label>
		{#if kind === 'credential'}
			<Input
				{id}
				type="password"
				mono
				class="mt-2"
				autocomplete="off"
				placeholder={credentialIsSet(field) ? m.secret_value_placeholder() : (field.placeholder ?? field.default ?? '')}
				disabled={clearedCredentials[field.key] ?? false}
				bind:value={() => credentialValues[field.key] ?? '', (value) => (credentialValues[field.key] = value)}
			/>
			{#if credentialIsSaved(field) && !field.required}
				<div class="mt-2">
					<SwitchWithLabel
						id={`${id}-clear`}
						label={m.clear_saved_value()}
						bind:checked={() => clearedCredentials[field.key] ?? false, (value) => (clearedCredentials[field.key] = value)}
					/>
				</div>
			{/if}
		{:else if field.multiline}
			<Textarea
				{id}
				rows={4}
				mono
				class="mt-2"
				placeholder={field.placeholder ?? field.default ?? ''}
				bind:value={() => inputValues[field.key] ?? '', (value) => (inputValues[field.key] = value)}
			/>
		{:else}
			<Input
				{id}
				type="text"
				mono
				class="mt-2"
				placeholder={field.placeholder ?? field.default ?? ''}
				bind:value={() => inputValues[field.key] ?? '', (value) => (inputValues[field.key] = value)}
			/>
		{/if}
		{#if field.description}
			<p class="mt-1 text-xs text-muted-foreground">{field.description}</p>
		{/if}
		{#if error}
			<p class="mt-1 text-sm text-destructive">{error}</p>
		{/if}
	</div>
{/snippet}
