<script lang="ts">
	import { Button } from '$lib/components/ui/button/index.js';
	import ArrowLeftIcon from '@lucide/svelte/icons/arrow-left';
	import TerminalIcon from '@lucide/svelte/icons/terminal';
	import CopyIcon from '@lucide/svelte/icons/copy';
	import InfoIcon from '@lucide/svelte/icons/info';
	import { Spinner } from '$lib/components/ui/spinner/index.js';
	import LayoutTemplateIcon from '@lucide/svelte/icons/layout-template';
	import WandIcon from '@lucide/svelte/icons/wand';
	import { Label } from '$lib/components/ui/label/index.js';
	import { goto, invalidateAll } from '$app/navigation';
	import { toast } from 'svelte-sonner';
	import { preventDefault, createForm } from '$lib/utils/form.utils';
	import { tryCatch } from '$lib/utils/try-catch';
	import { handleApiResultWithCallbacks } from '$lib/utils/api.util';
	import { Textarea } from '$lib/components/ui/textarea/index.js';
	import * as Dialog from '$lib/components/ui/dialog/index.js';
	import * as Tooltip from '$lib/components/ui/tooltip/index.js';
	import TemplateSelectionDialog from '$lib/components/dialogs/template-selection-dialog.svelte';
	import type { Template } from '$lib/types/template.type';
	import { z } from 'zod/v4';
	import { arcaneButtonVariants, actionConfigs } from '$lib/components/arcane-button/variants';
	import PlusCircleIcon from '@lucide/svelte/icons/plus-circle';
	import { m } from '$lib/paraglide/messages';
	import { projectService } from '$lib/services/project-service.js';
	import { systemService } from '$lib/services/system-service.js';
	import { templateService } from '$lib/services/template-service.js';
	import * as ButtonGroup from '$lib/components/ui/button-group/index.js';
	import * as DropdownMenu from '$lib/components/ui/dropdown-menu/index.js';
	import ChevronDown from '@lucide/svelte/icons/chevron-down';
	import CodePanel from '../components/CodePanel.svelte';
	import EditableName from '../components/EditableName.svelte';

	let { data } = $props();

	let saving = $state(false);
	let converting = $state(false);
	let creatingTemplate = $state(false);
	let showTemplateDialog = $state(false);
	let showConverterDialog = $state(false);
	let isLoadingTemplateContent = $state(false);

	const formSchema = z.object({
		name: z
			.string()
			.min(1, m.compose_project_name_required())
			.regex(/^[a-z0-9-_]+$/i, m.compose_project_name_invalid()),
		composeContent: z.string().min(1, m.compose_compose_content_required()),
		envContent: z.string().optional().default('')
	});

	const initialName = $derived(
		data.selectedTemplate ? data.selectedTemplate.name.toLowerCase().replace(/[^a-z0-9-_]/g, '-') : ''
	);

	let formData = $derived({
		name: initialName,
		composeContent: data.defaultTemplate || '',
		envContent: data.envTemplate || ''
	});

	let { inputs, ...form } = $derived(createForm<typeof formSchema>(formSchema, formData));

	let dockerRunCommand = $state('');
	let composeOpen = $state(true);
	let envOpen = $state(true);

	let nameInputRef = $state<HTMLInputElement | null>(null);

	async function handleSubmit() {
		await handleCreateProject();
	}

	async function handleCreateProject() {
		const validated = form.validate();
		if (!validated) return;

		const { name, composeContent, envContent } = validated;

		handleApiResultWithCallbacks({
			result: await tryCatch(projectService.createProject(name, composeContent, envContent)),
			message: m.common_create_failed({ resource: `${m.resource_project()} "${name}"` }),
			setLoadingState: (value) => (saving = value),
			onSuccess: async (project) => {
				toast.success(m.common_create_success({ resource: `${m.resource_project()} "${name}"` }));
				goto(`/projects/${project.id}`, { invalidateAll: true });
			}
		});
	}

	async function handleConvertDockerRun() {
		if (!dockerRunCommand.trim()) {
			toast.error(m.compose_enter_docker_run_command());
			return;
		}

		handleApiResultWithCallbacks({
			result: await tryCatch(systemService.convert(dockerRunCommand)),
			message: m.compose_convert_failed(),
			setLoadingState: (value) => (converting = value),
			onSuccess: (data) => {
				$inputs.composeContent.value = data.dockerCompose;
				$inputs.envContent.value = data.envVars;
				$inputs.name.value = data.serviceName;

				toast.success(m.compose_convert_success());
				dockerRunCommand = '';
				showConverterDialog = false;
			}
		});
	}

	async function handleTemplateSelect(template: Template) {
		showTemplateDialog = false;

		$inputs.composeContent.value = template.content ?? '';
		$inputs.envContent.value = template.envContent ?? '';

		if (!$inputs.name.value?.trim()) {
			$inputs.name.value = template.name.toLowerCase().replace(/[^a-z0-9-_]/g, '-');
		}
		toast.success(m.compose_template_loaded({ name: template.name }));
	}

	const exampleCommands = [m.compose_example_command_1(), m.compose_example_command_2(), m.compose_example_command_3()];

	function useExample(command: string) {
		dockerRunCommand = command;
	}

	async function handleCreateTemplate() {
		const validated = form.validate();
		if (!validated) return;

		const { name, composeContent, envContent } = validated;

		handleApiResultWithCallbacks({
			result: await tryCatch(
				templateService.createTemplate({
					name,
					content: composeContent,
					envContent
				})
			),
			message: m.common_create_failed({ resource: `${m.resource_template()} "${name}"` }),
			setLoadingState: (value) => (creatingTemplate = value),
			onSuccess: async () => {
				toast.success(m.common_create_success({ resource: `${m.resource_template()} "${name}"` }));
			}
		});
	}

	const templateBtnClass = arcaneButtonVariants({
		tone: actionConfigs.template?.tone ?? 'outline-primary',
		size: 'default'
	});

	const dropdownContentClass =
		'arcane-dd-content min-w-[220px] overflow-visible rounded-lg border border-primary/30 bg-background/95 ' +
		'backdrop-blur supports-[backdrop-filter]:bg-background/80 ring-1 ring-inset ring-primary/20 shadow-sm p-1';

	const dropdownItemClass =
		'flex cursor-pointer select-none items-center gap-2 rounded-md px-3 py-2 text-sm ' +
		'text-foreground/90 outline-none transition-colors ' +
		'hover:bg-primary/10 focus:bg-primary/10 ' +
		'data-[disabled]:opacity-50 data-[disabled]:pointer-events-none';
</script>

<div class="bg-background flex min-h-0 flex-col">
	<div class="sticky top-0 border-b">
		<div class="mx-auto flex h-16 max-w-full items-center justify-between gap-4 px-6">
			<div class="flex items-center gap-4">
				<Button variant="ghost" size="sm" href="/projects" class="gap-2 bg-transparent">
					<ArrowLeftIcon class="size-4" />
					{m.common_back()}
				</Button>
				<div class="bg-border hidden h-4 w-px sm:block"></div>
				<div class="hidden items-center gap-3 sm:flex">
					<EditableName
						bind:value={$inputs.name.value}
						bind:ref={nameInputRef}
						variant="inline"
						error={$inputs.name.error ?? undefined}
						originalValue=""
						placeholder={m.compose_project_name_placeholder?.() || 'Enter project name...'}
						canEdit={!saving && !isLoadingTemplateContent}
						class="hidden sm:block"
					/>
				</div>
			</div>

			<div class="flex items-center gap-2">
				<ButtonGroup.Root>
					<Tooltip.Provider>
						<Tooltip.Root open={!$inputs.name.value && !saving && !converting && !isLoadingTemplateContent ? undefined : false}>
							<Tooltip.Trigger>
								{#snippet child({ props })}
									<span {...props}>
										<Button
											disabled={!$inputs.name.value ||
												!$inputs.composeContent.value ||
												saving ||
												converting ||
												isLoadingTemplateContent}
											onclick={() => handleSubmit()}
											class={`${templateBtnClass} gap-2 rounded-r-none hover:translate-y-0 focus:translate-y-0 active:translate-y-0`}
										>
											{#if saving}
												<Spinner class="size-4" />
												{m.common_action_creating()}
											{:else}
												<PlusCircleIcon class="size-4" />
												{m.compose_create_project()}
											{/if}
										</Button>
									</span>
								{/snippet}
							</Tooltip.Trigger>
							{#if $inputs.name.value === ''}
								<Tooltip.Content class="arcane-tooltip-content max-w-[280px]">
									<p class="mb-1 text-sm font-medium">{m.compose_project_name_tooltip_title()}</p>
									<p class="text-muted-foreground text-xs">
										{m.compose_project_name_tooltip_description()}
									</p>
									<p class="bg-muted mt-1.5 inline-block rounded px-1.5 py-0.5 font-mono text-xs">
										{m.compose_project_name_tooltip_example()}
									</p>
								</Tooltip.Content>
							{/if}
						</Tooltip.Root>
					</Tooltip.Provider>

					<DropdownMenu.Root>
						<DropdownMenu.Trigger>
							{#snippet child({ props })}
								<Button
									{...props}
									class={`${templateBtnClass} -ml-px rounded-l-none px-2 hover:translate-y-0 focus:translate-y-0 active:translate-y-0`}
									variant="outline"
								>
									<ChevronDown class="size-4" />
								</Button>
							{/snippet}
						</DropdownMenu.Trigger>
						<DropdownMenu.Content align="end" class={dropdownContentClass}>
							<DropdownMenu.Group>
								<DropdownMenu.Item
									class={dropdownItemClass}
									disabled={saving || converting || isLoadingTemplateContent}
									onclick={() => (showTemplateDialog = true)}
								>
									<LayoutTemplateIcon class="size-4" />
									{m.common_use_template()}
								</DropdownMenu.Item>
								<DropdownMenu.Item class={dropdownItemClass} onclick={() => (showConverterDialog = true)}>
									<TerminalIcon class="size-4" />
									{m.compose_convert_from_docker_run()}
								</DropdownMenu.Item>
								<DropdownMenu.Separator />
								<DropdownMenu.Item
									class={dropdownItemClass}
									disabled={!$inputs.name.value ||
										!$inputs.composeContent.value ||
										saving ||
										converting ||
										creatingTemplate ||
										isLoadingTemplateContent}
									onclick={handleCreateTemplate}
								>
									{#if creatingTemplate}
										<Spinner class="size-4" />
									{:else}
										<WandIcon class="size-4" />
									{/if}
									{m.templates_create_template()}
								</DropdownMenu.Item>
							</DropdownMenu.Group>
						</DropdownMenu.Content>
					</DropdownMenu.Root>
				</ButtonGroup.Root>
			</div>
		</div>
	</div>

	<div class="flex-1 overflow-hidden">
		<div class="mx-auto h-full max-w-full">
			<div class="flex h-full flex-col gap-4 p-6">
				<div class="block sm:hidden">
					<EditableName
						bind:value={$inputs.name.value}
						bind:ref={nameInputRef}
						variant="block"
						error={$inputs.name.error ?? undefined}
						originalValue=""
						placeholder={m.compose_project_name_placeholder()}
						canEdit={!saving && !isLoadingTemplateContent}
					/>
				</div>

				<form
					class="grid h-full grid-cols-1 gap-4 lg:grid-cols-5 lg:items-stretch"
					style="grid-template-rows: 1fr;"
					onsubmit={preventDefault(handleSubmit)}
				>
					<div class="flex h-full flex-col lg:col-span-3">
						<CodePanel
							bind:open={composeOpen}
							title={m.compose_compose_file_title()}
							language="yaml"
							bind:value={$inputs.composeContent.value}
							placeholder={m.compose_compose_placeholder()}
							error={$inputs.composeContent.error ?? undefined}
						/>
					</div>

					<div class="flex h-full flex-col lg:col-span-2">
						<CodePanel
							bind:open={envOpen}
							title={m.compose_env_title()}
							language="env"
							bind:value={$inputs.envContent.value}
							placeholder={m.compose_env_placeholder()}
							error={$inputs.envContent.error ?? undefined}
						/>
					</div>
				</form>
			</div>
		</div>
	</div>
</div>

<Dialog.Root bind:open={showConverterDialog}>
	<Dialog.Content class="max-h-[80vh] sm:max-w-[800px]">
		<Dialog.Header>
			<Dialog.Title>{m.compose_converter_title()}</Dialog.Title>
			<Dialog.Description>{m.compose_converter_description()}</Dialog.Description>
		</Dialog.Header>

		<div class="max-h-[60vh] space-y-4 overflow-y-auto">
			<div class="space-y-2">
				<Label for="dockerRunCommand">{m.compose_docker_run_command_label()}</Label>
				<Textarea
					id="dockerRunCommand"
					bind:value={dockerRunCommand}
					placeholder={m.compose_docker_run_placeholder()}
					rows={3}
					disabled={converting}
					class="font-mono text-sm"
				/>
			</div>

			<div class="space-y-2">
				<Label class="text-muted-foreground text-xs">{m.compose_example_commands_label()}</Label>
				<div class="space-y-1">
					{#each exampleCommands as command}
						<Button
							type="button"
							variant="ghost"
							size="sm"
							class="h-auto w-full justify-start p-2 text-left font-mono text-xs break-all whitespace-normal"
							onclick={() => useExample(command)}
						>
							<CopyIcon class="mr-2 size-3 shrink-0" />
							<span>{command}</span>
						</Button>
					{/each}
				</div>
			</div>
		</div>

		<div class="flex w-full justify-end pt-4">
			<Button type="button" disabled={!dockerRunCommand.trim() || converting} onclick={handleConvertDockerRun}>
				{#if converting}
					<Spinner class="mr-2 size-4" />
					{m.compose_converting()}
				{:else}
					<WandIcon class="mr-2 size-4" />
					{m.compose_convert_action()}
				{/if}
			</Button>
		</div>
	</Dialog.Content>
</Dialog.Root>

<TemplateSelectionDialog
	bind:open={showTemplateDialog}
	templates={data.composeTemplates || []}
	onSelect={handleTemplateSelect}
	onDownloadSuccess={invalidateAll}
/>

<style>
	:global(.arcane-dd-content [data-arrow]) {
		background: color-mix(in srgb, var(--background), transparent 5%);
		border: 1px solid color-mix(in srgb, var(--primary), transparent 70%);
		box-shadow: 0 1px 2px 0 rgb(0 0 0 / 0.12);
		z-index: 10;
	}
</style>
