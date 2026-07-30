<script lang="ts">
	import * as ResponsiveDialog from '$lib/components/ui/responsive-dialog/index.js';
	import SheetFooterActions from '$lib/components/sheets/sheet-footer-actions.svelte';
	import SwitchWithLabel from '$lib/components/form/labeled-switch.svelte';
	import EnvironmentMultiSelect from '$lib/components/sheets/environment-multi-select.svelte';
	import { Button } from '$lib/components/ui/button';
	import { Input } from '$lib/components/ui/input';
	import { Label } from '$lib/components/ui/label';
	import { Textarea } from '$lib/components/ui/textarea';
	import * as RadioGroup from '$lib/components/ui/radio-group/index.js';
	import type { Environment } from '$lib/types/environment';
	import type { GlobalVariable, GlobalVariableCreateDto, GlobalVariableUpdateDto } from '$lib/types/variable';
	import { parseEnvText, normalizeVariableKeyInput } from '$lib/utils/env-file';
	import { z } from 'zod/v4';
	import { createForm, preventDefault } from '$lib/utils/settings';
	import * as m from '$lib/paraglide/messages.js';

	type VariableFormPayload =
		| { mode: 'create'; variable: GlobalVariableCreateDto }
		| { mode: 'edit'; id: string; variable: GlobalVariableUpdateDto }
		| { mode: 'bulk'; variables: GlobalVariableCreateDto[] };

	let {
		open = $bindable(false),
		variableToEdit = $bindable(null),
		environments,
		existingVariables = [],
		isLoading = false,
		onSubmit
	}: {
		open: boolean;
		variableToEdit: GlobalVariable | null;
		environments: Environment[];
		existingVariables?: GlobalVariable[];
		isLoading?: boolean;
		onSubmit: (payload: VariableFormPayload) => void;
	} = $props();

	const isEditMode = $derived(!!variableToEdit);

	let entryMode = $state<'single' | 'bulk'>('single');
	let scope = $state<'all' | 'specific'>('all');
	let selectedEnvIds = $state<string[]>([]);
	let scopeError = $state<string | null>(null);
	let bulkText = $state('');
	let bulkSecret = $state(false);
	let bulkError = $state<string | null>(null);

	const keyPattern = /^[A-Z_][A-Z0-9_]*$/i;

	// Mirrors the backend conflict rule: a key may exist once per overlapping
	// scope. The same key as both an all-environments variable and an
	// env-scoped variable is the override mechanism and is allowed.
	function scopeConflicts(candidateKey: string): boolean {
		const normalized = candidateKey.trim().toUpperCase();
		const candidateAll = scope === 'all';
		return existingVariables.some((existing) => {
			if (variableToEdit && existing.id === variableToEdit.id) return false;
			if (existing.key.toUpperCase() !== normalized) return false;
			if (candidateAll !== existing.allEnvironments) return false;
			if (candidateAll) return true;
			return existing.environmentIds.some((id) => selectedEnvIds.includes(id));
		});
	}

	const formSchema = $derived.by(() => {
		const wasSecret = variableToEdit?.isSecret ?? false;

		return z
			.object({
				key: z
					.string()
					.min(1, m.common_field_required({ field: m.key() }))
					.regex(keyPattern, m.invalid_key_format())
					.refine((key) => !scopeConflicts(key), m.variables_duplicate_keys_error()),
				value: z.string(),
				isSecret: z.boolean()
			})
			.superRefine((data, ctx) => {
				// A stored secret is never sent back to the client, so turning the
				// secret flag off requires the user to provide a readable value.
				if (wasSecret && !data.isSecret && data.value.trim() === '') {
					ctx.addIssue({ code: 'custom', path: ['value'], message: m.secret_new_value_required() });
				}
			});
	});

	const formData = $derived({
		key: variableToEdit?.key ?? '',
		value: variableToEdit ? (variableToEdit.isSecret ? '' : variableToEdit.value) : '',
		isSecret: variableToEdit?.isSecret ?? false
	});

	let { inputs, ...form } = $derived(createForm<typeof formSchema>(formSchema, formData));

	const parsed = $derived(parseEnvText(bulkText));

	const bulkDuplicateKeys = $derived.by(() => {
		// Entries in one paste share a single scope, so repeats within the batch
		// always conflict; against existing variables the check is scope-aware.
		const seen = new Set<string>();
		const duplicates = new Set<string>();
		for (const entry of parsed.entries) {
			if (seen.has(entry.key) || scopeConflicts(entry.key)) duplicates.add(entry.key);
			seen.add(entry.key);
		}
		return [...duplicates];
	});

	$effect(() => {
		if (open) {
			const editing = variableToEdit;
			entryMode = 'single';
			bulkText = '';
			bulkSecret = false;
			bulkError = null;
			scopeError = null;
			scope = editing && !editing.allEnvironments && editing.environmentIds.length > 0 ? 'specific' : 'all';
			selectedEnvIds = editing ? [...editing.environmentIds] : [];
		}
	});

	function validateScope(): boolean {
		scopeError = scope === 'specific' && selectedEnvIds.length === 0 ? m.select_at_least_one_environment() : null;
		return !scopeError;
	}

	function scopeDto(): Pick<GlobalVariableCreateDto, 'allEnvironments' | 'environmentIds'> {
		return scope === 'all'
			? { allEnvironments: true, environmentIds: [] }
			: { allEnvironments: false, environmentIds: [...selectedEnvIds] };
	}

	function handleSubmit() {
		const scopeValid = validateScope();

		if (entryMode === 'bulk' && !isEditMode) {
			bulkError = null;
			if (parsed.entries.length === 0) {
				bulkError = m.paste_env_empty_preview();
			} else if (bulkDuplicateKeys.length > 0) {
				bulkError = m.variables_duplicate_keys_error();
			}
			if (bulkError || !scopeValid) return;

			onSubmit({
				mode: 'bulk',
				variables: parsed.entries.map((entry) => ({
					key: entry.key,
					value: entry.value,
					isSecret: bulkSecret,
					...scopeDto()
				}))
			});
			return;
		}

		const data = form.validate();
		if (!data || !scopeValid) return;

		if (isEditMode && variableToEdit) {
			const dto: GlobalVariableUpdateDto = { key: data.key, isSecret: data.isSecret, ...scopeDto() };
			// Editing a secret with an empty value keeps the stored value.
			if (!(variableToEdit.isSecret && data.value === '')) dto.value = data.value;
			onSubmit({ mode: 'edit', id: variableToEdit.id, variable: dto });
		} else {
			onSubmit({ mode: 'create', variable: { key: data.key, value: data.value, isSecret: data.isSecret, ...scopeDto() } });
		}
	}

	function handleOpenChange(newOpenState: boolean) {
		open = newOpenState;
		if (!newOpenState) {
			variableToEdit = null;
		}
	}
</script>

<ResponsiveDialog.Root
	{open}
	onOpenChange={handleOpenChange}
	variant="sheet"
	title={isEditMode ? m.edit_variable() : m.create_variable()}
	description={isEditMode ? (variableToEdit?.key ?? '') : m.common_add_description()}
	contentClass="sm:max-w-[540px]"
>
	{#snippet children()}
		<form onsubmit={preventDefault(handleSubmit)} class="grid gap-4 py-6">
			{#if !isEditMode}
				<div class="inline-flex w-fit items-center gap-1 rounded-lg border border-border/50 bg-muted/30 p-1">
					<Button
						type="button"
						size="sm"
						variant={entryMode === 'single' ? 'secondary' : 'ghost'}
						class="h-7"
						onclick={() => (entryMode = 'single')}
					>
						{m.single()}
					</Button>
					<Button
						type="button"
						size="sm"
						variant={entryMode === 'bulk' ? 'secondary' : 'ghost'}
						class="h-7"
						onclick={() => (entryMode = 'bulk')}
					>
						{m.paste_env()}
					</Button>
				</div>
			{/if}

			{#if entryMode === 'bulk' && !isEditMode}
				<div>
					<Label for="variable-bulk-text">{m.paste_env_content()}</Label>
					<Textarea
						id="variable-bulk-text"
						rows={10}
						class="mt-2 font-mono text-sm"
						placeholder={m.paste_env_placeholder()}
						bind:value={bulkText}
					/>
				</div>

				<div class="rounded-md border border-border/50">
					{#if parsed.entries.length === 0}
						<p class="px-3 py-4 text-sm text-muted-foreground">{m.paste_env_empty_preview()}</p>
					{:else}
						<ul class="max-h-48 divide-y divide-border/50 overflow-y-auto">
							{#each parsed.entries as entry, index (index)}
								<li class="flex items-center gap-3 px-3 py-1.5 text-sm">
									<span class="font-mono font-medium">{entry.key}</span>
									<span class="min-w-0 flex-1 truncate text-right font-mono text-muted-foreground">
										{bulkSecret ? '••••••••' : entry.value}
									</span>
								</li>
							{/each}
						</ul>
					{/if}
				</div>

				<div class="text-xs text-muted-foreground">
					<span>{m.count_parsed({ count: parsed.entries.length })}</span>
					{#if parsed.invalidLines.length > 0}
						<span> &middot; {m.count_lines_could_not_be_parsed({ count: parsed.invalidLines.length })}</span>
					{/if}
				</div>

				{#if bulkDuplicateKeys.length > 0}
					<p class="text-sm text-destructive">
						{m.variables_duplicate_keys_error()} ({bulkDuplicateKeys.join(', ')})
					</p>
				{:else if bulkError}
					<p class="text-sm text-destructive">{bulkError}</p>
				{/if}

				<SwitchWithLabel
					id="variable-bulk-secret"
					label={m.secret()}
					description={m.secret_description()}
					bind:checked={bulkSecret}
				/>

				{@render scopeSelector()}
			{:else}
				<div>
					<Label for="variable-key">{m.key()}</Label>
					<Input
						id="variable-key"
						type="text"
						class="mt-2 font-mono"
						placeholder={m.key_placeholder()}
						bind:value={$inputs.key.value}
						oninput={(e) => normalizeVariableKeyInput(e, (value) => ($inputs.key.value = value))}
					/>
					{#if $inputs.key.error}
						<p class="mt-1 text-sm text-destructive">{$inputs.key.error}</p>
					{/if}
				</div>

				<div>
					<Label for="variable-value">{m.value()}</Label>
					<Textarea
						id="variable-value"
						rows={3}
						class="mt-2 font-mono"
						placeholder={variableToEdit?.isSecret ? m.secret_value_placeholder() : m.value_placeholder()}
						bind:value={$inputs.value.value}
					/>
					{#if $inputs.value.error}
						<p class="mt-1 text-sm text-destructive">{$inputs.value.error}</p>
					{/if}
				</div>

				<SwitchWithLabel
					id="variable-secret"
					label={m.secret()}
					description={m.secret_description()}
					bind:checked={$inputs.isSecret.value}
				/>

				{@render scopeSelector()}
			{/if}
		</form>
	{/snippet}

	{#snippet footer()}
		<SheetFooterActions
			bind:open
			cancelDisabled={isLoading}
			submitAction={isEditMode ? 'save' : 'create'}
			submitDisabled={isLoading}
			submitLoading={isLoading}
			onSubmit={handleSubmit}
			submitLabel={isEditMode ? m.common_save_changes() : m.common_add_button({ resource: m.variable() })}
		/>
	{/snippet}
</ResponsiveDialog.Root>

{#snippet scopeSelector()}
	<div>
		<Label class="mb-0">{m.common_scope()}</Label>
		<RadioGroup.Root
			class="mt-2 gap-2"
			value={scope}
			onValueChange={(value) => {
				scope = value as 'all' | 'specific';
				scopeError = null;
			}}
		>
			<label class="flex cursor-pointer items-start gap-3 rounded-md border border-border/50 p-3 hover:bg-accent/40">
				<RadioGroup.Item value="all" class="mt-0.5" />
				<div class="grid gap-1 leading-none">
					<span class="text-sm font-medium">{m.all_environments()}</span>
					<span class="text-xs text-muted-foreground">{m.all_environments_description()}</span>
				</div>
			</label>
			<label class="flex cursor-pointer items-start gap-3 rounded-md border border-border/50 p-3 hover:bg-accent/40">
				<RadioGroup.Item value="specific" class="mt-0.5" />
				<div class="grid gap-1 leading-none">
					<span class="text-sm font-medium">{m.specific_environments()}</span>
					<span class="text-xs text-muted-foreground">{m.specific_environments_description()}</span>
				</div>
			</label>
		</RadioGroup.Root>
		{#if scope === 'specific'}
			<div class="mt-2">
				<EnvironmentMultiSelect {environments} bind:selected={selectedEnvIds} />
			</div>
		{/if}
		{#if scopeError}
			<p class="mt-1 text-sm text-destructive">{scopeError}</p>
		{/if}
	</div>
{/snippet}
