<script lang="ts">
	import { toast } from 'svelte-sonner';
	import * as ResponsiveDialog from '$lib/components/ui/responsive-dialog/index.js';
	import * as Tabs from '$lib/components/ui/tabs/index.js';
	import { ArcaneButton } from '$lib/components/arcane-button/index.js';
	import FormInput from '$lib/components/form/form-input.svelte';
	import UrlInput from '$lib/components/form/url-input.svelte';
	import { Spinner } from '$lib/components/ui/spinner/index.js';
	import { CopyButton } from '$lib/components/ui/copy-button';
	import type { CreateEnvironmentDTO } from '$lib/types/environment.type';
	import { z } from 'zod/v4';
	import { createForm, preventDefault } from '$lib/utils/form.utils';
	import { m } from '$lib/paraglide/messages';
	import { environmentManagementService } from '$lib/services/env-mgmt-service';
	import { RemoteEnvironmentIcon, EdgeConnectionIcon } from '$lib/icons';

	type NewEnvironmentSheetProps = {
		open: boolean;
		onEnvironmentCreated?: () => void;
	};

	let { open = $bindable(false), onEnvironmentCreated }: NewEnvironmentSheetProps = $props();

	type ConnectionMode = 'direct' | 'edge';
	let connectionMode = $state<ConnectionMode>('direct');

	let createdEnvironment = $state<{
		id: string;
		apiKey: string;
		name: string;
		apiUrl: string;
		isEdge: boolean;
		dockerRun?: string;
		dockerCompose?: string;
	} | null>(null);

	let isSubmittingNewAgent = $state(false);
	let isLoadingSnippets = $state(false);

	let newAgentUrlProtocol = $state<'https' | 'http'>('http');
	let newAgentUrlHost = $state('');

	// Direct mode form schema requires URL
	const directFormSchema = z.object({
		name: z.string().min(1, m.environments_name_required()).max(25, m.environments_name_too_long()),
		apiUrl: z.string().min(1, m.environments_server_url_required())
	});

	// Edge mode form schema only requires name
	const edgeFormSchema = z.object({
		name: z.string().min(1, m.environments_name_required()).max(25, m.environments_name_too_long())
	});

	const { inputs: directInputs, ...directForm } = createForm<typeof directFormSchema>(directFormSchema, {
		name: '',
		apiUrl: ''
	});

	const { inputs: edgeInputs, ...edgeForm } = createForm<typeof edgeFormSchema>(edgeFormSchema, {
		name: ''
	});

	// Reset on open/close
	$effect(() => {
		if (open) {
			createdEnvironment = null;
			connectionMode = 'direct';
			newAgentUrlProtocol = 'http';
			newAgentUrlHost = '';
			$directInputs.name.value = '';
			$directInputs.apiUrl.value = '';
			$edgeInputs.name.value = '';
		}
	});

	// Sync UrlInput value with form validation for direct mode
	$effect(() => {
		$directInputs.apiUrl.value = newAgentUrlHost;
	});

	async function handleDirectSubmit() {
		const data = directForm.validate();
		if (!data) return;

		try {
			isSubmittingNewAgent = true;
			const fullUrl = `${newAgentUrlProtocol}://${newAgentUrlHost}`;

			const dto: CreateEnvironmentDTO = {
				name: data.name,
				apiUrl: fullUrl,
				useApiKey: true,
				isEdge: false
			};

			await createEnvironmentAndFetchSnippets(dto, fullUrl, false);
		} catch (error) {
			toast.error(m.environments_create_failed());
			console.error(error);
		} finally {
			isSubmittingNewAgent = false;
		}
	}

	async function handleEdgeSubmit() {
		const data = edgeForm.validate();
		if (!data) return;

		try {
			isSubmittingNewAgent = true;
			const edgeApiHost = data.name
				.trim()
				.toLowerCase()
				.replace(/[^a-z0-9]+/g, '-')
				.replace(/(^-|-$)+/g, '');
			const edgeApiUrl = `edge://${edgeApiHost}`;

			// Edge agents don't need a URL - they connect outbound
			// We use a placeholder URL that indicates edge mode
			const dto: CreateEnvironmentDTO = {
				name: data.name,
				apiUrl: edgeApiUrl,
				useApiKey: true,
				isEdge: true
			};

			await createEnvironmentAndFetchSnippets(dto, '', true);
		} catch (error) {
			toast.error(m.environments_create_failed());
			console.error(error);
		} finally {
			isSubmittingNewAgent = false;
		}
	}

	async function createEnvironmentAndFetchSnippets(dto: CreateEnvironmentDTO, apiUrl: string, isEdge: boolean) {
		const created = await environmentManagementService.create(dto);

		if (created.apiKey) {
			createdEnvironment = {
				id: created.id,
				apiKey: created.apiKey,
				name: created.name,
				apiUrl: apiUrl,
				isEdge: isEdge
			};

			// Fetch deployment snippets from backend
			isLoadingSnippets = true;
			try {
				const snippets = await environmentManagementService.getDeploymentSnippets(created.id);
				createdEnvironment.dockerRun = snippets.dockerRun;
				createdEnvironment.dockerCompose = snippets.dockerCompose;
			} catch (err) {
				console.error('Failed to fetch deployment snippets:', err);
			} finally {
				isLoadingSnippets = false;
			}

			toast.success(m.environments_created_success());
		} else {
			toast.error('Failed to generate API key');
		}
	}

	function handleDone() {
		onEnvironmentCreated?.();
		open = false;
	}
</script>

<ResponsiveDialog.Root
	bind:open
	variant="sheet"
	title={createdEnvironment ? m.environments_created_title() : m.environments_create_new_agent()}
	description={createdEnvironment ? m.environments_created_description() : m.environments_create_new_agent_description()}
	contentClass="sm:max-w-2xl"
>
	{#snippet children()}
		<div class="space-y-6 px-6 py-6">
			{#if createdEnvironment}
				<div class="space-y-4">
					{#if createdEnvironment.isEdge}
						<div class="bg-primary/10 text-primary flex items-center gap-2 rounded-lg p-3 text-sm">
							<EdgeConnectionIcon class="size-5" />
							<span>Edge agent - connects outbound to manager</span>
						</div>
					{/if}

					<div class="space-y-2">
						<div class="text-sm font-medium">{m.environments_api_key()}</div>
						<div class="flex items-center gap-2">
							<code class="bg-muted flex-1 rounded-md px-3 py-2 font-mono text-sm break-all">
								{createdEnvironment.apiKey}
							</code>
							{#if createdEnvironment.apiKey}
								<CopyButton text={createdEnvironment.apiKey} size="icon" class="size-7" />
							{/if}
						</div>
						<p class="text-muted-foreground text-xs">{m.environments_api_key_warning()}</p>
					</div>

					{#if isLoadingSnippets}
						<div class="flex items-center justify-center py-8">
							<Spinner class="size-6" />
						</div>
					{:else if createdEnvironment.dockerRun && createdEnvironment.dockerCompose}
						<div class="space-y-2">
							<div class="text-sm font-medium">{m.environments_docker_run_command()}</div>
							<div class="relative">
								<pre class="bg-muted overflow-x-auto rounded-md p-3 text-xs"><code>{createdEnvironment.dockerRun}</code></pre>
								<div class="absolute top-2 right-2">
									<CopyButton text={createdEnvironment.dockerRun} size="icon" class="size-7" />
								</div>
							</div>
						</div>

						<div class="space-y-2">
							<div class="text-sm font-medium">{m.environments_docker_compose()}</div>
							<div class="relative">
								<pre class="bg-muted overflow-x-auto rounded-md p-3 text-xs"><code>{createdEnvironment.dockerCompose}</code></pre>
								<div class="absolute top-2 right-2">
									<CopyButton text={createdEnvironment.dockerCompose} size="icon" class="size-7" />
								</div>
							</div>
						</div>
					{/if}

					<ArcaneButton action="base" class="w-full" onclick={handleDone} customLabel={m.common_done()} />
				</div>
			{:else}
				<Tabs.Root bind:value={connectionMode} class="w-full">
					<Tabs.List class="grid w-full grid-cols-2">
						<Tabs.Trigger value="direct" class="flex items-center gap-2">
							<RemoteEnvironmentIcon class="size-4" />
							Direct
						</Tabs.Trigger>
						<Tabs.Trigger value="edge" class="flex items-center gap-2">
							<EdgeConnectionIcon class="size-4" />
							Edge
						</Tabs.Trigger>
					</Tabs.List>

					<Tabs.Content value="direct" class="mt-4">
						<p class="text-muted-foreground mb-4 text-sm">
							Manager connects directly to the agent. Requires the agent port to be accessible.
						</p>
						<form onsubmit={preventDefault(handleDirectSubmit)} class="space-y-4">
							<FormInput
								label={m.common_name()}
								placeholder={m.environments_production_docker()}
								bind:input={$directInputs.name}
							/>

							<UrlInput
								id="new-agent-api-url"
								label={m.environments_agent_address()}
								placeholder={m.environments_agent_address_placeholder()}
								description={m.environments_agent_address_description()}
								bind:value={newAgentUrlHost}
								bind:protocol={newAgentUrlProtocol}
								disabled={isSubmittingNewAgent}
								required
								error={$directInputs.apiUrl.error ?? undefined}
							/>

							<ArcaneButton
								action="confirm"
								type="submit"
								class="w-full"
								disabled={isSubmittingNewAgent}
								loading={isSubmittingNewAgent}
								customLabel={m.environments_generate_config()}
							/>
						</form>
					</Tabs.Content>

					<Tabs.Content value="edge" class="mt-4">
						<p class="text-muted-foreground mb-4 text-sm">
							Agent connects outbound to the manager. No exposed ports required - ideal for firewalled environments.
						</p>
						<form onsubmit={preventDefault(handleEdgeSubmit)} class="space-y-4">
							<FormInput label={m.common_name()} placeholder="Remote Docker Host" bind:input={$edgeInputs.name} />

							<ArcaneButton
								action="confirm"
								type="submit"
								class="w-full"
								disabled={isSubmittingNewAgent}
								loading={isSubmittingNewAgent}
								customLabel={m.environments_generate_config()}
							/>
						</form>
					</Tabs.Content>
				</Tabs.Root>
			{/if}
		</div>
	{/snippet}
</ResponsiveDialog.Root>
