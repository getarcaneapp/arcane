<script lang="ts">
	import { Button } from '$lib/components/ui/button';
	import * as Card from '$lib/components/ui/card';
	import { toast } from 'svelte-sonner';
	import CodeEditor from '$lib/components/monaco-code-editor/editor.svelte';
	import { createForm } from '$lib/utils/form.utils';
	import { tryCatch } from '$lib/utils/try-catch';
	import { handleApiResultWithCallbacks } from '$lib/utils/api.util';
	import { m } from '$lib/paraglide/messages';
	import { templateService } from '$lib/services/template-service';
	import { z } from 'zod/v4';
	import { goto } from '$app/navigation';
	import TemplateSelectionDialog from '$lib/components/dialogs/template-selection-dialog.svelte';
	import { untrack } from 'svelte';
	import type { Template } from '$lib/types/template.type';
	import { ArrowLeftIcon, CodeIcon, VariableIcon, SaveIcon } from '$lib/icons';

	let { data } = $props();

	let saving = $state(false);
	let showTemplateDialog = $state(false);
	let isLoadingTemplate = $state(false);
	let originalComposeContent = $state(untrack(() => data.composeTemplate));
	let originalEnvContent = $state(untrack(() => data.envTemplate));

	const formSchema = z.object({
		composeContent: z.string().min(1, m.compose_compose_content_required()),
		envContent: z.string().optional().default('')
	});

	let formData = $derived({
		composeContent: originalComposeContent,
		envContent: originalEnvContent
	});

	let { inputs, ...form } = $derived(createForm<typeof formSchema>(formSchema, formData));

	const hasChanges = $derived(
		$inputs.composeContent.value !== originalComposeContent || $inputs.envContent.value !== originalEnvContent
	);

	async function handleSave() {
		const validated = form.validate();
		if (!validated) {
			toast.error(m.templates_validation_error());
			return;
		}

		const { composeContent, envContent } = validated;

		handleApiResultWithCallbacks({
			result: await tryCatch(templateService.saveDefaultTemplates(composeContent, envContent)),
			message: m.templates_save_failed(),
			setLoadingState: (value) => (saving = value),
			onSuccess: async () => {
				toast.success(m.templates_save_success());
				originalComposeContent = $inputs.composeContent.value;
				originalEnvContent = $inputs.envContent.value;
			}
		});
	}

	async function handleReset() {
		$inputs.composeContent.value = originalComposeContent;
		$inputs.envContent.value = originalEnvContent;
		toast.info(m.templates_reset_success());
	}

	async function handleTemplateSelect(template: Template) {
		showTemplateDialog = false;
		isLoadingTemplate = true;

		try {
			const templateContent = await templateService.getTemplateContent(template.id);
			$inputs.composeContent.value = templateContent.content ?? template.content ?? '';
			$inputs.envContent.value = templateContent.envContent ?? template.envContent ?? '';
			toast.success(m.compose_template_loaded({ name: template.name }));
		} catch (error) {
			console.error('Error loading template:', error);
			toast.error(error instanceof Error ? error.message : m.templates_download_failed());
		} finally {
			isLoadingTemplate = false;
		}
	}
</script>

<div class="container mx-auto flex h-full min-h-0 max-w-full flex-col gap-6 overflow-hidden p-2 pb-10 sm:p-6 sm:pb-10">
	<div class="space-y-3 sm:space-y-4">
		<Button variant="ghost" onclick={() => goto('/customize/templates')} class="w-fit gap-2">
			<ArrowLeftIcon class="size-4" />
			<span>{m.common_back_to({ resource: m.templates_title() })}</span>
		</Button>

		<div class="flex flex-col justify-between gap-4 sm:flex-row sm:items-center">
			<div>
				<h1 class="text-xl font-bold wrap-break-word sm:text-2xl">{m.templates_defaults_title()}</h1>
				<p class="text-muted-foreground mt-1.5 text-sm wrap-break-word sm:text-base">
					{m.templates_defaults_description()}
				</p>
			</div>
			<div class="flex flex-col gap-2 sm:flex-row">
				<Button variant="outline" onclick={() => (showTemplateDialog = true)} disabled={saving || isLoadingTemplate}>
					{m.common_use_template()}
				</Button>
				<Button variant="outline" onclick={handleReset} disabled={!hasChanges || saving || isLoadingTemplate}>
					{m.common_reset()}
				</Button>
				<Button onclick={handleSave} disabled={!hasChanges || saving || isLoadingTemplate} class="gap-2">
					<SaveIcon class="size-4" />
					{saving ? m.common_action_saving() : m.common_save()}
				</Button>
			</div>
		</div>
	</div>

	<div class="flex min-h-0 flex-1 flex-col gap-6 lg:grid lg:grid-cols-5 lg:grid-rows-1 lg:items-stretch">
		<Card.Root class="flex min-h-0 min-w-0 flex-1 flex-col lg:col-span-3">
			<Card.Header icon={CodeIcon} class="shrink-0">
				<div class="flex flex-col space-y-1.5">
					<Card.Title>
						<h2>{m.templates_compose_template_label()}</h2>
					</Card.Title>
					<Card.Description>{m.templates_service_definitions()}</Card.Description>
				</div>
			</Card.Header>
			<Card.Content class="flex min-h-0 min-w-0 flex-1 flex-col p-0">
				<div class="min-h-0 min-w-0 flex-1 rounded-b-xl">
					<CodeEditor
						bind:value={$inputs.composeContent.value}
						language="yaml"
						readOnly={saving || isLoadingTemplate}
						fontSize="13px"
					/>
				</div>
			</Card.Content>
			{#if $inputs.composeContent.error}
				<Card.Footer class="pt-0">
					<p class="text-destructive text-xs font-medium">
						{$inputs.composeContent.error}
					</p>
				</Card.Footer>
			{/if}
		</Card.Root>

		<Card.Root class="flex min-h-0 min-w-0 flex-1 flex-col lg:col-span-2">
			<Card.Header icon={VariableIcon} class="shrink-0">
				<div class="flex flex-col space-y-1.5">
					<Card.Title>
						<h2>{m.templates_env_template_label()}</h2>
					</Card.Title>
					<Card.Description>{m.templates_default_config_values()}</Card.Description>
				</div>
			</Card.Header>
			<Card.Content class="flex min-h-0 min-w-0 flex-1 flex-col p-0">
				<div class="min-h-0 min-w-0 flex-1 rounded-b-xl">
					<CodeEditor
						bind:value={$inputs.envContent.value}
						language="env"
						readOnly={saving || isLoadingTemplate}
						fontSize="13px"
					/>
				</div>
			</Card.Content>
			{#if $inputs.envContent.error}
				<Card.Footer class="pt-0">
					<p class="text-destructive text-xs font-medium">
						{$inputs.envContent.error}
					</p>
				</Card.Footer>
			{/if}
		</Card.Root>
	</div>
</div>

<TemplateSelectionDialog bind:open={showTemplateDialog} templates={data.templates || []} onSelect={handleTemplateSelect} />
