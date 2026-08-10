<script lang="ts">
	import * as ResponsiveDialog from '#lib/components/ui/responsive-dialog/index.js';
	import SheetFooterActions from '#lib/components/sheets/sheet-footer-actions.svelte';
	import SwitchWithLabel from '#lib/components/form/labeled-switch.svelte';
	import SelectWithLabel from '#lib/components/form/select-with-label.svelte';
	import SearchableSelect from '#lib/components/form/searchable-select.svelte';
	import { Button } from '#lib/components/ui/button';
	import { Input } from '#lib/components/ui/input';
	import { Label } from '#lib/components/ui/label';
	import { Textarea } from '#lib/components/ui/textarea';
	import CodeEditor from '#lib/components/code-editor/editor.svelte';
	import { createQuery } from '@tanstack/svelte-query';
	import { containerService } from '#lib/services/container-service';
	import { queryKeys } from '#lib/query/query-keys';
	import type {
		Snippet,
		SnippetCreateDto,
		SnippetUpdateDto,
		SnippetParameterDef,
		SnippetParameterType,
		SnippetTarget
	} from '#lib/types/snippet';
	import { preventDefault } from '#lib/utils/settings';
	import { m } from '#lib/paraglide/messages';
	import { AddIcon, TrashIcon } from '#lib/icons';

	type SnippetFormPayload =
		| { mode: 'create'; snippet: SnippetCreateDto }
		| { mode: 'edit'; id: string; snippet: SnippetUpdateDto };

	let {
		open = $bindable(false),
		snippetToEdit = $bindable(null),
		environmentId,
		isLoading = false,
		onSubmit,
		onDelete
	}: {
		open: boolean;
		snippetToEdit: Snippet | null;
		environmentId: string;
		isLoading?: boolean;
		onSubmit: (payload: SnippetFormPayload) => void;
		onDelete?: (snippet: Snippet) => void;
	} = $props();

	interface ParamRow {
		key: number;
		name: string;
		type: SnippetParameterType;
		label: string;
		required: boolean;
		default: string;
		optionsText: string;
		pattern: string;
	}

	const isEditMode = $derived(!!snippetToEdit);
	let rowKeySeq = 0;

	let name = $state('');
	let description = $state('');
	let script = $state('');
	let target = $state<SnippetTarget>('host');
	let containerId = $state('');
	let workingDir = $state('');
	let timeoutSec = $state<number | ''>(60);
	let paramRows = $state<ParamRow[]>([]);
	let scheduleEnabled = $state(false);
	let schedule = $state('');
	let scheduleError = $state<string | null>(null);
	let scheduleParamValues = $state<Record<string, string>>({});
	let formError = $state<string | null>(null);

	const cronExamples = [
		{ label: m.jobs_cron_example_every_15min(), value: '0 */15 * * * *' },
		{ label: m.jobs_cron_example_hourly(), value: '0 0 * * * *' },
		{ label: m.jobs_cron_example_every_6hours(), value: '0 0 */6 * * *' },
		{ label: m.jobs_cron_example_daily(), value: '0 0 0 * * *' }
	];

	function paramDefToRow(def: SnippetParameterDef): ParamRow {
		return {
			key: rowKeySeq++,
			name: def.name,
			type: def.type,
			label: def.label ?? '',
			required: def.required ?? false,
			default: def.default ?? '',
			optionsText: (def.options ?? []).join(', '),
			pattern: def.pattern ?? ''
		};
	}

	const containersQuery = createQuery(() => {
		const queryEnvId = environmentId;
		return {
			queryKey: queryKeys.containers.list(queryEnvId, { pagination: { page: 1, limit: 200 } }),
			queryFn: () => containerService.getContainersForEnvironment(queryEnvId, { pagination: { page: 1, limit: 200 } }),
			enabled: open
		};
	});
	const containerItems = $derived(
		(containersQuery.data?.data ?? [])
			.filter((c) => c.state === 'running')
			.map((c) => ({ value: c.id, label: c.names[0]?.replace(/^\//, '') || c.id.slice(0, 12) }))
	);

	$effect(() => {
		if (open) {
			const editing = snippetToEdit;
			name = editing?.name ?? '';
			description = editing?.description ?? '';
			script = editing?.script ?? '';
			target = editing?.target ?? 'host';
			containerId = editing?.containerId ?? '';
			workingDir = editing?.workingDir ?? '';
			timeoutSec = editing?.timeoutSec ?? 60;
			paramRows = (editing?.parameters ?? []).map(paramDefToRow);
			scheduleEnabled = editing?.scheduleEnabled ?? false;
			schedule = editing?.schedule ?? '';
			scheduleParamValues = { ...(editing?.scheduleParameters ?? {}) };
			scheduleError = null;
			formError = null;
		}
	});

	function addParamRow() {
		paramRows = [
			...paramRows,
			{ key: rowKeySeq++, name: '', type: 'string', label: '', required: false, default: '', optionsText: '', pattern: '' }
		];
	}

	function removeParamRow(key: number) {
		paramRows = paramRows.filter((row) => row.key !== key);
	}

	function useCronExample(value: string) {
		schedule = value;
		scheduleError = null;
	}

	function buildParameters(): SnippetParameterDef[] | null {
		const defs: SnippetParameterDef[] = [];
		const seen = new Set<string>();
		for (const row of paramRows) {
			const trimmedName = row.name.trim();
			if (!trimmedName) continue;
			if (!/^[A-Za-z_][A-Za-z0-9_]{0,63}$/.test(trimmedName)) {
				formError = m.snippets_invalid_parameter_name({ name: trimmedName });
				return null;
			}
			if (seen.has(trimmedName)) {
				formError = m.snippets_duplicate_parameter_name({ name: trimmedName });
				return null;
			}
			seen.add(trimmedName);

			const def: SnippetParameterDef = { name: trimmedName, type: row.type };
			if (row.label.trim()) def.label = row.label.trim();
			if (row.required) def.required = true;
			if (row.default.trim()) def.default = row.default.trim();
			if (row.type === 'select') {
				def.options = row.optionsText
					.split(',')
					.map((o) => o.trim())
					.filter(Boolean);
				if (def.options.length === 0) {
					formError = m.snippets_select_requires_options({ name: trimmedName });
					return null;
				}
			}
			if (row.type === 'string' && row.pattern.trim()) def.pattern = row.pattern.trim();
			defs.push(def);
		}
		return defs;
	}

	function handleSubmit() {
		formError = null;

		if (!name.trim()) {
			formError = m.common_field_required({ field: m.common_name() });
			return;
		}
		if (!script.trim()) {
			formError = m.common_field_required({ field: m.snippets_script() });
			return;
		}
		if (target === 'container' && !containerId) {
			formError = m.snippets_target_container_required();
			return;
		}

		const parameters = buildParameters();
		if (parameters === null) return;

		if (scheduleEnabled) {
			const parts = schedule.trim().split(/\s+/);
			if (parts.length !== 6) {
				scheduleError = m.jobs_cron_invalid();
				return;
			}
		}
		scheduleError = null;

		const scheduleParameters: Record<string, string> = {};
		for (const def of parameters) {
			scheduleParameters[def.name] = scheduleParamValues[def.name] ?? '';
		}

		const base = {
			name: name.trim(),
			description: description.trim() || undefined,
			script,
			target,
			containerId: target === 'container' ? containerId : undefined,
			parameters,
			workingDir: workingDir.trim() || undefined,
			timeoutSec: typeof timeoutSec === 'number' ? timeoutSec : undefined,
			schedule: schedule.trim() || undefined,
			scheduleEnabled,
			scheduleParameters
		};

		if (isEditMode && snippetToEdit) {
			onSubmit({ mode: 'edit', id: snippetToEdit.id, snippet: base });
		} else {
			onSubmit({ mode: 'create', snippet: { ...base, name: base.name, script: base.script } as SnippetCreateDto });
		}
	}

	function handleOpenChange(newOpenState: boolean) {
		open = newOpenState;
		if (!newOpenState) {
			snippetToEdit = null;
		}
	}
</script>

<ResponsiveDialog.Root
	{open}
	onOpenChange={handleOpenChange}
	variant="sheet"
	title={isEditMode ? m.snippets_edit_title() : m.snippets_create_title()}
	description={m.snippets_form_description()}
	contentClass="sm:max-w-[640px]"
>
	{#snippet children()}
		<form onsubmit={preventDefault(handleSubmit)} class="grid gap-4 py-6">
			<div>
				<Label for="snippet-name">{m.common_name()}</Label>
				<Input id="snippet-name" class="mt-2" bind:value={name} />
			</div>

			<div>
				<Label for="snippet-description">{m.common_description()}</Label>
				<Textarea id="snippet-description" rows={2} class="mt-2" bind:value={description} />
			</div>

			<div>
				<Label for="snippet-script">{m.snippets_script()}</Label>
				<p class="mt-0.5 text-xs text-muted-foreground">{m.snippets_script_help()}</p>
				<div class="mt-2 h-64 overflow-hidden rounded-md border border-border/50">
					<CodeEditor bind:value={script} language="shell" autoHeight={false} />
				</div>
			</div>

			<div class="grid grid-cols-2 gap-4">
				<div>
					<Label for="snippet-target">{m.snippets_target()}</Label>
					<div class="mt-2">
						<SelectWithLabel
							id="snippet-target"
							label=""
							hideLabel
							bind:value={target as string}
							options={[
								{ label: m.snippets_target_host(), value: 'host' },
								{ label: m.snippets_target_container(), value: 'container' }
							]}
						/>
					</div>
				</div>
				{#if target === 'container'}
					<div>
						<Label for="snippet-target-container">{m.snippets_target_container()}</Label>
						<SearchableSelect
							triggerId="snippet-target-container"
							class="mt-2 w-full"
							items={containerItems}
							bind:value={containerId}
							selectText={m.snippets_target_container_placeholder()}
							onSelect={(value) => (containerId = value)}
						/>
					</div>
				{/if}
			</div>

			<div class="grid grid-cols-2 gap-4">
				<div>
					<Label for="snippet-working-dir">{m.snippets_working_dir()}</Label>
					<Input id="snippet-working-dir" class="mt-2 font-mono" placeholder="/" bind:value={workingDir} />
				</div>
				<div>
					<Label for="snippet-timeout">{m.snippets_timeout_sec()}</Label>
					<Input id="snippet-timeout" type="number" class="mt-2" min="1" max="300" bind:value={timeoutSec} />
				</div>
			</div>

			<div class="space-y-3">
				<div class="flex items-center justify-between">
					<Label class="mb-0">{m.snippets_parameters()}</Label>
					<Button type="button" variant="outline" size="sm" onclick={addParamRow}>
						<AddIcon class="size-4" />
						{m.snippets_add_parameter()}
					</Button>
				</div>
				<p class="text-xs text-muted-foreground">{m.snippets_parameters_help()}</p>

				{#each paramRows as row (row.key)}
					<div class="grid gap-2 rounded-md border border-border/50 p-3">
						<div class="grid grid-cols-2 gap-2">
							<Input placeholder={m.snippets_parameter_name_placeholder()} class="font-mono" bind:value={row.name} />
							<SelectWithLabel
								id={`param-type-${row.key}`}
								label=""
								hideLabel
								bind:value={row.type as string}
								options={[
									{ label: m.snippets_param_type_string(), value: 'string' },
									{ label: m.snippets_param_type_number(), value: 'number' },
									{ label: m.snippets_param_type_boolean(), value: 'boolean' },
									{ label: m.snippets_param_type_select(), value: 'select' }
								]}
							/>
						</div>
						<div class="grid grid-cols-2 gap-2">
							<Input placeholder={m.snippets_parameter_label_placeholder()} bind:value={row.label} />
							<Input placeholder={m.snippets_parameter_default_placeholder()} bind:value={row.default} />
						</div>
						{#if row.type === 'select'}
							<Input placeholder={m.snippets_parameter_options_placeholder()} bind:value={row.optionsText} />
						{/if}
						{#if row.type === 'string'}
							<Input placeholder={m.snippets_parameter_pattern_placeholder()} class="font-mono" bind:value={row.pattern} />
						{/if}
						<div class="flex items-center justify-between">
							<SwitchWithLabel
								id={`param-required-${row.key}`}
								label={m.snippets_parameter_required()}
								bind:checked={row.required}
							/>
							<Button type="button" variant="ghost" size="icon" onclick={() => removeParamRow(row.key)}>
								<TrashIcon class="size-4" />
							</Button>
						</div>
					</div>
				{/each}
			</div>

			<div class="space-y-3 rounded-md border border-border/50 p-3">
				<SwitchWithLabel
					id="snippet-schedule-enabled"
					label={m.snippets_schedule_enabled()}
					description={m.snippets_schedule_enabled_description()}
					bind:checked={scheduleEnabled}
				/>
				{#if scheduleEnabled}
					<div>
						<Label for="snippet-schedule">{m.jobs_cron_expression()}</Label>
						<Input id="snippet-schedule" class="mt-2 font-mono" placeholder="0 */15 * * * *" bind:value={schedule} />
						{#if scheduleError}
							<p class="mt-1 text-sm text-destructive">{scheduleError}</p>
						{:else}
							<p class="mt-1 text-xs text-muted-foreground">{m.jobs_cron_expression_help()}</p>
						{/if}
					</div>
					<div class="grid grid-cols-2 gap-2">
						{#each cronExamples as example (example.value)}
							<Button
								type="button"
								variant="outline"
								size="sm"
								onclick={() => useCronExample(example.value)}
								class="h-auto items-start justify-start px-3 py-2 whitespace-normal"
							>
								<div class="text-left">
									<div class="text-xs leading-4 font-medium">{example.label}</div>
									<div class="font-mono text-xs leading-4 text-muted-foreground">{example.value}</div>
								</div>
							</Button>
						{/each}
					</div>
					{#if paramRows.length > 0}
						<div class="space-y-2">
							<Label class="mb-0">{m.snippets_schedule_parameters()}</Label>
							<p class="text-xs text-muted-foreground">{m.snippets_schedule_parameters_help()}</p>
							{#each paramRows as row (row.key)}
								{#if row.name.trim()}
									<Input placeholder={row.label || row.name} bind:value={scheduleParamValues[row.name.trim()]} />
								{/if}
							{/each}
						</div>
					{/if}
				{/if}
			</div>

			{#if formError}
				<p class="text-sm text-destructive">{formError}</p>
			{/if}
		</form>
	{/snippet}

	{#snippet footer()}
		<div class="flex w-full flex-col gap-2">
			<SheetFooterActions
				bind:open
				cancelDisabled={isLoading}
				submitAction={isEditMode ? 'save' : 'create'}
				submitDisabled={isLoading}
				submitLoading={isLoading}
				onSubmit={handleSubmit}
				submitLabel={isEditMode ? m.common_save_changes() : m.common_add_button({ resource: m.snippets_singular() })}
			/>
			{#if isEditMode && snippetToEdit && onDelete}
				<Button
					type="button"
					variant="destructive"
					class="w-full"
					disabled={isLoading}
					onclick={() => onDelete?.(snippetToEdit!)}
				>
					<TrashIcon class="size-4" />
					{m.common_delete()}
				</Button>
			{/if}
		</div>
	{/snippet}
</ResponsiveDialog.Root>
