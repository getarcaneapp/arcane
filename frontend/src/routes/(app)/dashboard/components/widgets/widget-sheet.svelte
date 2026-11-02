<script lang="ts" module>
	import { z } from 'zod/v4';

	import { m } from '#lib/paraglide/messages.js';
	import type { CustomDashboardWidgetType } from '#lib/types/auth.js';
	import { createForm } from '#lib/utils/settings.svelte.js';

	import { MAX_RESOURCE_LIMIT, RESOURCE_KIND_KEYS } from './resource-kinds';

	const MAX_TITLE_LENGTH = 80;
	const MAX_SEARCH_LENGTH = 256;

	const WIDGET_OPTIONS: { type: CustomDashboardWidgetType; description: () => string }[] = [
		{ type: 'resourceQuery', description: m.filtered_resources_description },
		{ type: 'resource', description: m.resource_widget_description },
		{ type: 'containerStats', description: m.container_stats_widget_description },
		{ type: 'containerLogs', description: m.container_logs_widget_description },
		{ type: 'environment', description: m.environment_widget_description }
	];

	export function widgetFormSchema() {
		const limitError = m.whole_number_range({ min: 1, max: MAX_RESOURCE_LIMIT });
		return z
			.object({
				type: z.enum(WIDGET_OPTIONS.map((option) => option.type)),
				title: z.string().max(MAX_TITLE_LENGTH, m.common_max_length({ field: m.title(), maxLength: MAX_TITLE_LENGTH })),
				environmentIds: z.array(z.string()),
				resourceKind: z.enum(RESOURCE_KIND_KEYS).nullable(),
				resourceId: z.string(),
				search: z.string().max(MAX_SEARCH_LENGTH, m.common_max_length({ field: m.search(), maxLength: MAX_SEARCH_LENGTH })),
				filters: z.record(z.string(), z.string()),
				display: z.enum(['count', 'list']).nullable(),
				limit: z.number({ error: limitError }).int(limitError).min(1, limitError).max(MAX_RESOURCE_LIMIT, limitError).nullable()
			})
			.check((ctx) => {
				const value = ctx.value;
				const require = (path: string, field: string) =>
					ctx.issues.push({ code: 'custom', message: m.common_field_required({ field }), path: [path], input: value });
				if (value.type !== 'resourceQuery' && !value.environmentIds[0]) {
					require('environmentIds', m.resource_environment_cap());
				}
				if (value.type !== 'environment' && !value.resourceKind) {
					require('resourceKind', m.resource_type());
				}
				if (value.type !== 'resourceQuery' && value.type !== 'environment' && !value.resourceId) {
					require('resourceId', m.resource());
				}
			});
	}

	export type WidgetFormSchema = ReturnType<typeof widgetFormSchema>;
	export type WidgetFormState = ReturnType<typeof createForm<WidgetFormSchema>>;
</script>

<script lang="ts">
	import { ArcaneButton } from '#lib/components/arcane-button/index.js';
	import { Label } from '#lib/components/ui/label/index.js';
	import * as RadioGroup from '#lib/components/ui/radio-group/index.js';
	import * as ResponsiveDialog from '#lib/components/ui/responsive-dialog/index.js';
	import type { DashboardWidgetConfig } from '#lib/types/auth.js';
	import {
		createDashboardWidget,
		DASHBOARD_WIDGET_TYPES,
		type DashboardWidget,
		type DashboardWidgetInput
	} from '#lib/utils/dashboard-layout.js';
	import { preventDefault } from '#lib/utils/settings.svelte.js';

	import ResourceQueryConfig from './resource-query-config.svelte';
	import WidgetTargetConfig from './widget-target-config.svelte';

	let { onSave }: { onSave: (widget: DashboardWidgetInput) => void } = $props();

	let open = $state(false);
	let existing = $state.raw<DashboardWidget | null>(null);
	let step = $state<'type' | 'config'>('type');

	function formValues(type: CustomDashboardWidgetType, widget?: DashboardWidget): z.infer<WidgetFormSchema> {
		const defaults: DashboardWidgetConfig = type === 'environment' ? {} : { resourceKind: 'containers' };
		const config = widget ? (widget.config ?? {}) : defaults;
		return {
			type,
			title: widget?.title ?? '',
			environmentIds: config.environmentIds ?? [],
			resourceKind: config.resourceKind ?? null,
			resourceId: config.resourceId ?? '',
			search: config.search ?? '',
			filters: config.filters ?? {},
			display: config.display ?? null,
			limit: config.limit ?? null
		};
	}

	const form = createForm<WidgetFormSchema>(widgetFormSchema(), formValues('resourceQuery'));
	const type = $derived(form.inputs.type.value);

	export function show(widget?: DashboardWidget) {
		const configurable = WIDGET_OPTIONS.find((option) => option.type === widget?.type);
		existing = configurable ? (widget ?? null) : null;
		step = configurable ? 'config' : 'type';
		form.reset(formValues(configurable?.type ?? 'resourceQuery', configurable ? widget : undefined));
		open = true;
	}

	function next() {
		form.reset(formValues(type));
		step = 'config';
	}

	function save() {
		if (step === 'type') {
			next();
			return;
		}
		const data = form.validate();
		if (!data) return;
		const config: DashboardWidgetConfig = {
			environmentIds: data.environmentIds.length > 0 ? data.environmentIds : undefined,
			resourceKind: data.resourceKind ?? undefined,
			resourceId: data.resourceId || undefined,
			search: data.search || undefined,
			filters: Object.keys(data.filters).length > 0 ? data.filters : undefined,
			display: data.display ?? undefined,
			limit: data.limit ?? undefined
		};
		const title = data.title || undefined;
		onSave(existing ? { ...existing, config, title } : createDashboardWidget(data.type, config, title));
		open = false;
	}
</script>

<ResponsiveDialog.Root
	bind:open
	variant="sheet"
	title={existing ? m.edit_widget() : m.new_widget()}
	description={step === 'type' ? m.widget_type_description() : DASHBOARD_WIDGET_TYPES[type].label()}
	contentClass="sm:max-w-125"
>
	{#snippet children()}
		<form id="dashboard-widget-form" onsubmit={preventDefault(save)} novalidate class="grid gap-4 py-6">
			{#if step === 'type'}
				<div>
					<Label class="mb-0">{m.widget_type()}</Label>
					<RadioGroup.Root
						class="mt-2"
						value={type}
						onValueChange={(value) =>
							(form.inputs.type.value = WIDGET_OPTIONS.find((option) => option.type === value)?.type ?? type)}
					>
						<div class="grid gap-2">
							{#each WIDGET_OPTIONS as option (option.type)}
								<label class="flex cursor-pointer items-start gap-3 rounded-md border border-border/50 p-3 hover:bg-accent/40">
									<RadioGroup.Item value={option.type} class="mt-0.5" />
									<div class="grid gap-1 leading-none">
										<span class="text-sm font-medium">{DASHBOARD_WIDGET_TYPES[option.type].label()}</span>
										<span class="text-xs text-muted-foreground">{option.description()}</span>
									</div>
								</label>
							{/each}
						</div>
					</RadioGroup.Root>
				</div>
			{:else if type === 'resourceQuery'}
				<ResourceQueryConfig {form} />
			{:else if type === 'resource'}
				<WidgetTargetConfig
					{form}
					kinds={['containers', 'projects']}
					titlePlaceholder={DASHBOARD_WIDGET_TYPES.resource.label()}
				/>
			{:else if type === 'environment'}
				<WidgetTargetConfig {form} kinds={[]} titlePlaceholder={DASHBOARD_WIDGET_TYPES.environment.label()} />
			{:else}
				<WidgetTargetConfig {form} kinds={['containers']} titlePlaceholder={DASHBOARD_WIDGET_TYPES[type].label()} />
			{/if}
		</form>
	{/snippet}

	{#snippet footer()}
		<div class="flex w-full flex-row gap-2">
			{#if step === 'config' && !existing}
				<ArcaneButton
					action="base"
					tone="outline"
					type="button"
					class="flex-1"
					customLabel={m.common_back()}
					onclick={() => (step = 'type')}
				/>
			{:else}
				<ArcaneButton action="cancel" tone="outline" type="button" class="flex-1" onclick={() => (open = false)} />
			{/if}
			<ArcaneButton
				action={existing ? 'save' : 'create'}
				type="submit"
				form="dashboard-widget-form"
				class="flex-1"
				customLabel={step === 'type' ? m.common_next() : undefined}
			/>
		</div>
	{/snippet}
</ResponsiveDialog.Root>
