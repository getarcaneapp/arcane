<script lang="ts">
	import { ArcaneButton } from '$lib/components/arcane-button/index.js';
	import { ArrowLeftIcon, TerminalIcon, CopyIcon, InfoIcon, TemplateIcon, AddIcon, GitBranchIcon } from '$lib/icons';
	import { Spinner } from '$lib/components/ui/spinner/index.js';
	import { Label } from '$lib/components/ui/label/index.js';
	import { goto, invalidateAll } from '$app/navigation';
	import { toast } from 'svelte-sonner';
	import { preventDefault, createForm } from '$lib/utils/form.utils';
	import { tryCatch } from '$lib/utils/try-catch';
	import { handleApiResultWithCallbacks } from '$lib/utils/api.util';
	import { Textarea } from '$lib/components/ui/textarea/index.js';
	import * as Dialog from '$lib/components/ui/dialog/index.js';
	import * as ArcaneTooltip from '$lib/components/arcane-tooltip';
	import TemplateSelectionDialog from '$lib/components/dialogs/template-selection-dialog.svelte';
	import type { Template } from '$lib/types/template.type';
	import { z } from 'zod/v4';
	import { arcaneButtonVariants, actionConfigs } from '$lib/components/arcane-button/variants';
	import { m } from '$lib/paraglide/messages';
	import { projectService } from '$lib/services/project-service.js';
	import { systemService } from '$lib/services/system-service.js';
	import { templateService } from '$lib/services/template-service.js';
	import * as ButtonGroup from '$lib/components/ui/button-group/index.js';
	import * as DropdownMenu from '$lib/components/ui/dropdown-menu/index.js';
	import { ArrowDownIcon as ChevronDown } from '$lib/icons';
	import CodePanel from '../components/CodePanel.svelte';
	import EditableName from '../components/EditableName.svelte';
	import { environmentStore } from '$lib/stores/environment.store.svelte';

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
		size: 'default',
		hoverEffect: 'none'
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

<div class="bg-background flex h-full min-h-0 flex-col">
	<div class="sticky top-0 mb-2 border-b">
		<div class="mx-auto flex h-16 max-w-full items-center justify-between gap-4 px-6">
			<div class="flex items-center gap-4">
				<ArcaneButton
					action="base"
					tone="ghost"
					size="sm"
					href="/projects"
					class="gap-2 bg-transparent"
					icon={ArrowLeftIcon}
					customLabel={m.common_back()}
				/>
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
					<ArcaneTooltip.Root
						open={!$inputs.name.value && !saving && !converting && !isLoadingTemplateContent ? undefined : false}
					>
						<ArcaneTooltip.Trigger>
							<span>
								<ArcaneButton
									action="create"
									tone="ghost"
									disabled={!$inputs.name.value ||
										!$inputs.composeContent.value ||
										saving ||
										converting ||
										isLoadingTemplateContent}
									onclick={() => handleSubmit()}
									class={`${templateBtnClass} gap-2 rounded-r-none`}
									loading={saving}
									customLabel={m.compose_create_project()}
									loadingLabel={m.common_action_creating()}
								/>
							</span>
						</ArcaneTooltip.Trigger>
						<ArcaneTooltip.Content class="arcane-tooltip-content max-w-[280px]">
							{#if $inputs.name.value === ''}
								<p class="mb-1 text-sm font-medium">{m.compose_project_name_tooltip_title()}</p>
								<p class="text-muted-foreground text-xs">
									{m.compose_project_name_tooltip_description()}
								</p>
								<p class="bg-muted mt-1.5 inline-block rounded px-1.5 py-0.5 font-mono text-xs">
									{m.compose_project_name_tooltip_example()}
								</p>
							{/if}
						</ArcaneTooltip.Content>
					</ArcaneTooltip.Root>

					<DropdownMenu.Root>
						<DropdownMenu.Trigger>
							{#snippet child({ props })}
								<ArcaneButton
									{...props}
									action="base"
									tone="ghost"
									class={`${templateBtnClass} -ml-px rounded-l-none px-2`}
									icon={ChevronDown}
								/>
							{/snippet}
						</DropdownMenu.Trigger>
						<DropdownMenu.Content align="end" class={dropdownContentClass}>
							<DropdownMenu.Group>
								<DropdownMenu.Item
									class={dropdownItemClass}
									disabled={saving || converting || isLoadingTemplateContent}
									onclick={() => (showTemplateDialog = true)}
								>
									<TemplateIcon class="size-4" />
									{m.common_use_template()}
								</DropdownMenu.Item>
								<DropdownMenu.Item class={dropdownItemClass} onclick={() => (showConverterDialog = true)}>
									<TerminalIcon class="size-4" />
									{m.compose_convert_from_docker_run()}
								</DropdownMenu.Item>
								<DropdownMenu.Item
									class={dropdownItemClass}
									onclick={async () =>
										goto(`/environments/${await environmentStore.getCurrentEnvironmentId()}/gitops?action=create`)}
								>
									<GitBranchIcon class="size-4" />
									{m.git_from_git_repo()}
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
										<AddIcon class="size-4" />
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

	<div class="flex min-h-0 flex-1 overflow-hidden">
		<div class="mx-auto h-full w-full max-w-full min-w-0">
			<div class="flex h-full min-h-0 flex-col gap-4">
				<div class="block flex-shrink-0 py-4 sm:hidden">
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
					class="flex min-h-0 flex-1 flex-col gap-4 lg:grid lg:grid-cols-5 lg:grid-rows-1 lg:items-stretch"
					onsubmit={preventDefault(handleSubmit)}
				>
					<div class="flex min-h-0 flex-1 flex-col lg:col-span-3">
						<CodePanel
							bind:open={composeOpen}
							title={m.compose_compose_file_title()}
							language="yaml"
							bind:value={$inputs.composeContent.value}
							error={$inputs.composeContent.error ?? undefined}
						/>
					</div>

					<div class="flex min-h-0 flex-1 flex-col lg:col-span-2">
						<CodePanel
							bind:open={envOpen}
							title={m.compose_env_title()}
							language="env"
							bind:value={$inputs.envContent.value}
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
						<ArcaneButton
							action="base"
							tone="ghost"
							size="sm"
							class="h-auto w-full justify-start p-2 text-left font-mono text-xs break-all whitespace-normal"
							onclick={() => useExample(command)}
							icon={CopyIcon}
							customLabel={command}
						/>
					{/each}
				</div>
			</div>
		</div>

		<div class="flex w-full justify-end pt-4">
			<ArcaneButton
				action="create"
				disabled={!dockerRunCommand.trim() || converting}
				onclick={handleConvertDockerRun}
				loading={converting}
				customLabel={m.compose_convert_action()}
				loadingLabel={m.compose_converting()}
			/>
		</div>
	</Dialog.Content>
</Dialog.Root>

<TemplateSelectionDialog
	bind:open={showTemplateDialog}
	templates={data.composeTemplates || []}
	onSelect={handleTemplateSelect}
	onDownloadSuccess={invalidateAll}
/>
