<script lang="ts">
	import { goto } from '$app/navigation';
	import { useQueryClient } from '@tanstack/svelte-query';
	import { toast } from 'svelte-sonner';

	import { ArcaneButton } from '#lib/components/arcane-button/index.js';
	import CodePanel from '#lib/components/code-panel.svelte';
	import SelectWithLabel from '#lib/components/form/select-with-label.svelte';
	import TextInputWithLabel from '#lib/components/form/text-input-with-label.svelte';
	import EmptyState from '#lib/components/states/empty-state.svelte';
	import * as Card from '#lib/components/ui/card/index.js';
	import { ArrowLeftIcon, FileTextIcon } from '#lib/icons/index.js';
	import { m } from '#lib/paraglide/messages.js';
	import { queryKeys } from '#lib/query/query-keys.js';
	import { podmanService } from '#lib/services/podman-service.js';
	import { environmentStore } from '#lib/stores/environment.store.svelte.js';
	import { PODMAN_QUADLET_TYPES, type PodmanQuadletFile, type PodmanQuadletType } from '#lib/types/podman.js';
	import { handleApiResultWithCallbacks } from '#lib/utils/api.js';
	import { tryCatch } from '#lib/utils/try-catch.js';

	import QuadletExtraFiles from '../components/quadlet-extra-files.svelte';

	let { data } = $props();

	const queryClient = useQueryClient();
	const status = $derived(data.podmanStatus);
	const appsSupported = $derived(status?.quadletAppsSupported === true);

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

	let baseName = $state('');
	let quadletType = $state<PodmanQuadletType>('container');
	let content = $state(templates.container);
	let kubeYaml = $state('');
	let app = $state('');
	let extraFiles = $state<PodmanQuadletFile[]>([]);
	let submitting = $state(false);

	const fileName = $derived(`${baseName.trim()}.${quadletType}`);
	const kubeFileName = $derived(`${baseName.trim()}.yaml`);

	function handleTypeChange(value: string) {
		const next = value as PodmanQuadletType;
		if (content === templates[quadletType]) content = templates[next];
		quadletType = next;
	}

	async function handleSubmit() {
		if (!baseName.trim()) {
			toast.error(m.common_field_required({ field: m.common_name() }));
			return;
		}
		const files = [
			...(quadletType === 'kube' && kubeYaml.trim() ? [{ name: kubeFileName, content: kubeYaml }] : []),
			...(appsSupported ? extraFiles.filter((file) => file.name.trim()) : [])
		];
		submitting = true;
		await handleApiResultWithCallbacks({
			result: await tryCatch(
				podmanService.installQuadlet({
					name: fileName,
					content,
					extraFiles: files.length > 0 ? files : undefined,
					app: appsSupported && app.trim() ? app.trim() : undefined
				})
			),
			message: m.podman_quadlet_install_failed({ name: fileName }),
			setLoadingState: (value) => (submitting = value),
			onSuccess: async (report) => {
				const errors = Object.entries(report.quadletErrors ?? {});
				if (errors.length > 0) {
					toast.error(m.podman_quadlet_install_failed({ name: fileName }), {
						description: errors.map(([name, error]) => `${name}: ${error}`).join('\n')
					});
					return;
				}
				toast.success(m.podman_quadlet_install_success({ name: fileName }));
				await queryClient.invalidateQueries({ queryKey: queryKeys.podman.quadlets(environmentStore.selected?.id ?? '0') });
				await goto(`/podman/quadlets/${encodeURIComponent(fileName)}`);
			}
		});
	}
</script>

<div class="flex min-h-0 flex-col bg-background">
	<div class="sticky top-0 z-10 border-b bg-background">
		<div class="mx-auto flex h-16 w-full max-w-full items-center gap-4 px-6">
			<ArcaneButton
				action="base"
				tone="ghost"
				size="sm"
				href="/podman/quadlets"
				class="gap-2 bg-transparent"
				icon={ArrowLeftIcon}
				customLabel={m.common_back()}
			/>
			<div class="hidden h-4 w-px bg-border sm:block"></div>
			<h1 class="flex-1 text-base font-semibold">{m.podman_quadlet_install()}</h1>
			{#if status?.quadletsSupported}
				<ArcaneButton
					action="create"
					customLabel={m.podman_quadlet_install()}
					onclick={handleSubmit}
					loading={submitting}
					disabled={submitting}
				/>
			{/if}
		</div>
	</div>

	<div class="mx-auto w-full max-w-full space-y-6 px-6 py-6">
		{#if !status?.quadletsSupported}
			<EmptyState icon={FileTextIcon} title={m.common_unavailable()} description={m.podman_quadlets_unsupported()} />
		{:else}
			<Card.Root>
				<Card.Header>
					<Card.Title>{m.common_details_title({ resource: m.podman_quadlet() })}</Card.Title>
					<Card.Description>{m.podman_quadlet_install_description()}</Card.Description>
				</Card.Header>
				<Card.Content class="grid gap-4 md:grid-cols-2">
					<TextInputWithLabel
						id="quadlet-name"
						label={m.common_name()}
						helpText={fileName}
						bind:value={baseName}
						required
						disabled={submitting}
					/>
					<SelectWithLabel
						id="quadlet-type"
						label={m.common_type()}
						value={quadletType}
						options={PODMAN_QUADLET_TYPES.map((type) => ({ label: `.${type}`, value: type }))}
						onValueChange={handleTypeChange}
						disabled={submitting}
					/>
					{#if appsSupported}
						<TextInputWithLabel
							id="quadlet-app"
							label={m.podman_quadlet_app()}
							description={m.podman_quadlet_app_description()}
							bind:value={app}
							disabled={submitting}
						/>
					{/if}
				</Card.Content>
			</Card.Root>

			<CodePanel title={fileName} language="systemd" validationMode="none" bind:value={content} autoHeight />

			{#if quadletType === 'kube'}
				<CodePanel title={kubeFileName} language="yaml" validationMode="none" bind:value={kubeYaml} autoHeight />
			{/if}

			{#if appsSupported}
				<Card.Root>
					<Card.Header>
						<Card.Title>{m.podman_quadlet_extra_files()}</Card.Title>
						<Card.Description>{m.podman_quadlet_extra_files_description()}</Card.Description>
					</Card.Header>
					<Card.Content>
						<QuadletExtraFiles bind:files={extraFiles} disabled={submitting} />
					</Card.Content>
				</Card.Root>
			{/if}
		{/if}
	</div>
</div>
