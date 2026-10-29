<script lang="ts">
	import { useQueryClient } from '@tanstack/svelte-query';
	import { toast } from 'svelte-sonner';

	import { ArcaneButton } from '#lib/components/arcane-button/index.js';
	import EnvironmentDestinationSelect from '#lib/components/form/environment-destination-select.svelte';
	import EmptyState from '#lib/components/states/empty-state.svelte';
	import { Input } from '#lib/components/ui/input/index.js';
	import { Label } from '#lib/components/ui/label/index.js';
	import * as Select from '#lib/components/ui/select/index.js';
	import { ArrowLeftIcon, FileTextIcon } from '#lib/icons/index.js';
	import { m } from '#lib/paraglide/messages.js';
	import { queryKeys } from '#lib/query/query-keys.js';
	import { podmanService } from '#lib/services/podman-service.js';
	import { PODMAN_QUADLET_TYPES, type PodmanQuadletType } from '#lib/types/podman.js';
	import { handleApiResultWithCallbacks } from '#lib/utils/api.js';
	import { hasPermission } from '#lib/utils/auth.js';
	import { withBase, goto } from '#lib/utils/base-path.js';
	import { destinationEnvironmentId, environmentHref } from '#lib/utils/environments.js';
	import { shownPodmanEnvironments } from '#lib/utils/podman.js';
	import { tryCatch } from '#lib/utils/try-catch.js';

	import EditableName from '../../../projects/components/editable-name.svelte';
	import QuadletWorkspaceEditor, {
		createQuadletWorkspace,
		quadletWorkspaceFiles
	} from '../components/quadlet-workspace-editor.svelte';

	let { data } = $props();
	const queryClient = useQueryClient();

	const templates: Record<PodmanQuadletType, string> = {
		container: '[Unit]\nDescription=\n\n[Container]\nImage=\n\n[Install]\nWantedBy=default.target\n',
		pod: '[Pod]\nPodName=\n',
		kube: '[Kube]\nYaml=\n\n[Install]\nWantedBy=default.target\n',
		network: '[Network]\n',
		volume: '[Volume]\n',
		image: '[Image]\nImage=\n',
		build: '[Build]\nImageTag=\nSetWorkingDirectory=unit\n',
		artifact: '[Artifact]\nArtifact=\n'
	};

	const podmanEnvironments = $derived(shownPodmanEnvironments(data.podmanStatuses));
	const quadletEnvironments = $derived(shownPodmanEnvironments(data.podmanStatuses, (status) => status.quadletsSupported));
	const installEnvironments = $derived(quadletEnvironments.filter((env) => hasPermission('podman:quadlets', env.id)));
	let picked = $state('');
	const environmentId = $derived(destinationEnvironmentId(picked, 'podman:quadlets', installEnvironments));
	const status = $derived(data.podmanStatuses[environmentId]);
	const appsSupported = $derived(status?.quadletAppsSupported === true);

	let baseName = $state('');
	let nameInputRef = $state<HTMLInputElement | null>(null);
	let quadletType = $state<PodmanQuadletType>('container');
	let content = $state(templates.container);
	let app = $state('');
	let submitting = $state(false);
	const workspace = createQuadletWorkspace();

	const fileName = $derived(`${baseName.trim()}.${quadletType}`);
	const canSubmit = $derived(!!baseName.trim() && !!content.trim() && !submitting);

	function handleTypeChange(value: string) {
		const next = value as PodmanQuadletType;
		if (content === templates[quadletType]) content = templates[next];
		quadletType = next;
	}

	async function handleSubmit() {
		const installEnvironmentId = environmentId;
		const installFileName = fileName;
		const extraFiles = quadletWorkspaceFiles(workspace);
		submitting = true;
		await handleApiResultWithCallbacks({
			result: await tryCatch(
				podmanService.installQuadlet(
					{
						name: installFileName,
						content,
						extraFiles: extraFiles.length > 0 ? extraFiles : undefined,
						app: appsSupported && app.trim() ? app.trim() : undefined
					},
					installEnvironmentId
				)
			),
			message: m.podman_quadlet_install_failed({ name: installFileName }),
			setLoadingState: (value) => (submitting = value),
			onSuccess: async (report) => {
				const errors = Object.entries(report.quadletErrors ?? {});
				if (errors.length > 0) {
					toast.error(m.podman_quadlet_install_failed({ name: installFileName }), {
						description: errors.map(([name, error]) => `${name}: ${error}`).join('\n')
					});
					return;
				}
				toast.success(m.podman_quadlet_install_success({ name: installFileName }));
				await Promise.all([
					queryClient.invalidateQueries({ queryKey: queryKeys.podman.environment(installEnvironmentId) }),
					queryClient.invalidateQueries({ queryKey: ['podman-quadlets'] })
				]);
				await goto(environmentHref(`/podman/quadlets/${encodeURIComponent(installFileName)}`, installEnvironmentId));
			}
		});
	}
</script>

{#snippet nameField(variant: 'inline' | 'block')}
	<EditableName
		bind:value={baseName}
		bind:ref={nameInputRef}
		{variant}
		originalValue=""
		placeholder={m.podman_quadlet_name_placeholder()}
		canEdit={!submitting}
		class={variant === 'inline' ? 'hidden sm:block' : undefined}
	/>
{/snippet}

<div class="flex h-full min-h-0 flex-col bg-background">
	<div class="sticky top-0 mb-2 border-b">
		<div class="mx-auto flex h-16 max-w-full items-center justify-between gap-4 px-6">
			<div class="flex items-center gap-4">
				<ArcaneButton
					action="base"
					tone="ghost"
					size="sm"
					href={withBase('/podman/quadlets')}
					class="gap-2 bg-transparent"
					icon={ArrowLeftIcon}
					customLabel={m.common_back()}
				/>
				<div class="hidden h-4 w-px bg-border sm:block"></div>
				{#if quadletEnvironments.length > 0}
					<div class="hidden items-center gap-3 sm:flex">
						{@render nameField('inline')}
					</div>
				{:else}
					<h1 class="text-base font-semibold">{m.podman_quadlet_install()}</h1>
				{/if}
			</div>

			{#if quadletEnvironments.length > 0}
				<div class="flex items-center gap-2">
					<EnvironmentDestinationSelect
						bind:value={() => environmentId, (value) => (picked = value)}
						permission="podman:quadlets"
						environments={installEnvironments}
					/>
					{#if installEnvironments.length > 0}
						<ArcaneButton
							action="create"
							customLabel={m.podman_quadlet_install()}
							loadingLabel={m.common_action_creating()}
							onclick={handleSubmit}
							loading={submitting}
							disabled={!canSubmit}
						/>
					{/if}
				</div>
			{/if}
		</div>
	</div>

	{#if quadletEnvironments.length === 0}
		<div class="mx-auto w-full max-w-full px-6 py-6">
			<EmptyState
				icon={FileTextIcon}
				title={m.common_unavailable()}
				description={podmanEnvironments.length === 0 ? m.podman_environments_unavailable() : m.podman_quadlets_unsupported()}
			/>
		</div>
	{:else}
		<div class="flex min-h-0 flex-1 overflow-hidden">
			<div class="mx-auto h-full w-full max-w-full min-w-0">
				<div class="flex h-full min-h-0 flex-col gap-3">
					<div class="block flex-shrink-0 py-4 sm:hidden">
						{@render nameField('block')}
					</div>

					<div class="flex shrink-0 flex-wrap items-center gap-x-6 gap-y-3">
						<div class="flex items-center gap-2">
							<Label for="quadlet-type" weight="normal" class="shrink-0">{m.common_type()}</Label>
							<Select.Root type="single" value={quadletType} onValueChange={handleTypeChange} disabled={submitting}>
								<Select.Trigger id="quadlet-type" class="h-8 min-w-32">
									<span class="truncate">.{quadletType}</span>
								</Select.Trigger>
								<Select.Content>
									{#each PODMAN_QUADLET_TYPES as type (type)}
										<Select.Item value={type}>.{type}</Select.Item>
									{/each}
								</Select.Content>
							</Select.Root>
						</div>
						{#if appsSupported}
							<div class="flex items-center gap-2">
								<Label for="quadlet-app" weight="normal" class="shrink-0" title={m.podman_quadlet_app_description()}>
									{m.podman_quadlet_app()}
								</Label>
								<Input
									id="quadlet-app"
									class="h-8 w-72"
									placeholder={m.podman_quadlet_app_description()}
									bind:value={app}
									disabled={submitting}
								/>
							</div>
						{/if}
					</div>

					<QuadletWorkspaceEditor
						{workspace}
						mainFileName={fileName}
						bind:content
						fileId="podman:quadlets:new"
						{environmentId}
						disabled={submitting}
					/>
				</div>
			</div>
		</div>
	{/if}
</div>
