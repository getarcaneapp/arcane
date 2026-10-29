<script lang="ts">
	import { createQuery, useQueryClient } from '@tanstack/svelte-query';
	import { toast } from 'svelte-sonner';

	import { ArcaneButton } from '#lib/components/arcane-button/index.js';
	import CodePanel from '#lib/components/code-panel.svelte';
	import { openConfirmDialog } from '#lib/components/confirm-dialog/index.js';
	import EnvironmentDestinationSelect from '#lib/components/form/environment-destination-select.svelte';
	import LabeledSwitch from '#lib/components/form/labeled-switch.svelte';
	import SearchableSelect from '#lib/components/form/searchable-select.svelte';
	import SelectWithLabel from '#lib/components/form/select-with-label.svelte';
	import EmptyState from '#lib/components/states/empty-state.svelte';
	import { Badge } from '#lib/components/ui/badge/index.js';
	import * as ButtonGroup from '#lib/components/ui/button-group/index.js';
	import { Checkbox } from '#lib/components/ui/checkbox/index.js';
	import * as DropdownMenu from '#lib/components/ui/dropdown-menu/index.js';
	import { Label } from '#lib/components/ui/label/index.js';
	import { ResponsiveDialog } from '#lib/components/ui/responsive-dialog/index.js';
	import { ArrowDownIcon, ArrowLeftIcon, CloseIcon, LayersIcon, TerminalIcon, TrashIcon } from '#lib/icons/index.js';
	import { m } from '#lib/paraglide/messages.js';
	import { queryKeys } from '#lib/query/query-keys.js';
	import { containerService } from '#lib/services/container-service.js';
	import { networkService } from '#lib/services/network-service.js';
	import { podmanService } from '#lib/services/podman-service.js';
	import { volumeService } from '#lib/services/volume-service.js';
	import { PODMAN_KUBE_GENERATE_TYPES, type PodmanKubeGenerateType } from '#lib/types/podman.js';
	import { handleApiResultWithCallbacks } from '#lib/utils/api.js';
	import { withBase } from '#lib/utils/base-path.js';
	import { templateBtnClass } from '#lib/utils/compose-flow.js';
	import { destinationEnvironmentId } from '#lib/utils/environments.js';
	import { withOccurrenceKeys } from '#lib/utils/formatting.js';
	import { shownPodmanEnvironments } from '#lib/utils/podman.js';
	import { tryCatch } from '#lib/utils/try-catch.js';

	let { data } = $props();
	const queryClient = useQueryClient();

	const podmanEnvs = $derived(shownPodmanEnvironments(data.podmanStatuses));
	let picked = $state('');
	const environmentId = $derived(destinationEnvironmentId(picked, 'podman:kube', podmanEnvs));

	let yaml = $state('');
	let start = $state(true);
	let replace = $state(false);
	let networks = $state<string[]>([]);
	let generateOpen = $state(false);
	let generateNames = $state<string[]>([]);
	let generateType = $state<PodmanKubeGenerateType>('pod');
	let generateService = $state(false);
	let resultLines = $state<string[]>([]);
	let loading = $state({ play: false, down: false, generate: false });

	type PickerItem = { value: string; label: string; hint?: string };

	const listAll = { pagination: { page: 1, limit: -1 } };

	const networksQuery = createQuery(() => ({
		queryKey: [...queryKeys.podman.environment(environmentId), 'kube', 'networks'],
		queryFn: () => networkService.getNetworksForEnvironment(environmentId, listAll),
		enabled: podmanEnvs.length > 0
	}));

	const networkItems = $derived<PickerItem[]>(
		(networksQuery.data?.data ?? []).map((network) => ({ value: network.name, label: network.name }))
	);

	const generateSourcesQuery = createQuery(() => ({
		queryKey: [...queryKeys.podman.environment(environmentId), 'kube', 'generate-sources'],
		queryFn: async () => {
			const [pods, containers, volumes] = await Promise.all([
				podmanService.getPods(environmentId, listAll),
				containerService.getContainersForEnvironment(environmentId, listAll),
				volumeService.getVolumesForEnvironment(environmentId, listAll)
			]);
			return [
				...pods.data.map((pod) => ({ value: pod.name, label: pod.name, hint: m.podman_pod() })),
				...containers.data.map((container) => {
					const name = container.names[0]?.replace(/^\//, '') ?? container.id;
					return { value: name, label: name, hint: m.resource_container() };
				}),
				...volumes.data.map((volume) => ({ value: volume.name, label: volume.name, hint: m.resource_volume() }))
			].filter((item, index, all) => all.findIndex((other) => other.value === item.value) === index);
		},
		enabled: generateOpen
	}));

	$effect(() => {
		void environmentId;
		resultLines = [];
		networks = [];
		generateNames = [];
	});

	function toggleValue(selected: string[], value: string) {
		return selected.includes(value) ? selected.filter((entry) => entry !== value) : [...selected, value];
	}

	async function invalidatePods(targetId: string) {
		await Promise.all([
			queryClient.invalidateQueries({ queryKey: queryKeys.podman.environment(targetId) }),
			queryClient.invalidateQueries({ queryKey: ['podman-pods'] })
		]);
	}

	async function handlePlay() {
		if (!yaml.trim()) {
			toast.error(m.common_field_required({ field: m.podman_kube_yaml() }));
			return;
		}
		const targetId = environmentId;
		loading.play = true;
		await handleApiResultWithCallbacks({
			result: await tryCatch(
				podmanService.kubePlay({ yaml, start, replace, networks: networks.length > 0 ? networks : undefined }, targetId)
			),
			message: m.podman_kube_play_failed(),
			setLoadingState: (value) => (loading.play = value),
			onSuccess: async (report) => {
				if ((report.pods ?? []).some((pod) => (pod.containerErrors ?? []).length > 0)) {
					toast.error(m.podman_kube_play_partial({ count: report.pods?.length ?? 0 }));
				} else {
					toast.success(m.podman_kube_play_success({ count: report.pods?.length ?? 0 }));
				}
				await invalidatePods(targetId);
				if (targetId !== environmentId) return;
				resultLines = [
					...(report.pods ?? []).flatMap((pod) => [
						m.podman_kube_pod_created({ id: pod.id.slice(0, 12), count: pod.containers?.length ?? 0 }),
						...(pod.containerErrors ?? [])
					]),
					...(report.volumes ?? []).map((volume) => volume.name)
				];
			}
		});
	}

	function handleDown() {
		if (!yaml.trim()) {
			toast.error(m.common_field_required({ field: m.podman_kube_yaml() }));
			return;
		}
		const targetId = environmentId;
		const downYaml = yaml;
		openConfirmDialog({
			title: m.common_down(),
			message: m.podman_kube_down_confirm(),
			checkboxes: [{ id: 'force', label: m.podman_kube_force_description() }],
			confirm: {
				label: m.common_down(),
				destructive: true,
				action: async (checkboxStates) => {
					loading.down = true;
					await handleApiResultWithCallbacks({
						result: await tryCatch(podmanService.kubeDown({ yaml: downYaml, force: checkboxStates['force'] === true }, targetId)),
						message: m.podman_kube_down_failed(),
						setLoadingState: (value) => (loading.down = value),
						onSuccess: async (report) => {
							toast.success(m.podman_kube_down_success({ count: report.rmReport?.length ?? 0 }));
							await invalidatePods(targetId);
							if (targetId !== environmentId) return;
							resultLines = (report.rmReport ?? []).map((pod) => m.podman_kube_pod_removed({ id: pod.id.slice(0, 12) }));
						}
					});
				}
			}
		});
	}

	async function handleGenerate() {
		if (generateNames.length === 0) {
			toast.error(m.common_field_required({ field: m.podman_kube_generate_names() }));
			return;
		}
		const targetId = environmentId;
		loading.generate = true;
		await handleApiResultWithCallbacks({
			result: await tryCatch(
				podmanService.kubeGenerate({ names: generateNames, type: generateType, service: generateService || undefined }, targetId)
			),
			message: m.podman_kube_generate_failed(),
			setLoadingState: (value) => (loading.generate = value),
			onSuccess: (response) => {
				if (targetId !== environmentId) return;
				yaml = response.yaml;
				generateOpen = false;
			}
		});
	}
</script>

{#snippet multiSelect(
	id: string,
	items: PickerItem[],
	selected: string[],
	isLoading: boolean,
	setSelected: (value: string[]) => void
)}
	<div class="flex min-w-0 flex-wrap items-center gap-1.5">
		<SearchableSelect
			triggerId={id}
			items={items.map((item) => ({ ...item, selected: selected.includes(item.value) }))}
			closeOnSelect={false}
			{isLoading}
			onSelect={(value) => setSelected(toggleValue(selected, value))}
			class="min-w-48"
		/>
		{#each selected as name (name)}
			<Badge variant="secondary" mono>
				{name}
				<button
					type="button"
					aria-label={m.common_remove()}
					onclick={() => setSelected(selected.filter((entry) => entry !== name))}
				>
					<CloseIcon class="size-3" />
				</button>
			</Badge>
		{/each}
	</div>
{/snippet}

<div class="flex h-full min-h-0 flex-col bg-background">
	<div class="sticky top-0 mb-2 border-b">
		<div class="mx-auto flex h-16 max-w-full items-center justify-between gap-4 px-6">
			<div class="flex items-center gap-4">
				<ArcaneButton
					action="base"
					tone="ghost"
					size="sm"
					href={withBase('/podman/pods')}
					class="gap-2 bg-transparent"
					icon={ArrowLeftIcon}
					customLabel={m.common_back()}
				/>
				<div class="hidden h-4 w-px bg-border sm:block"></div>
				<div class="hidden items-center gap-2 sm:flex">
					<LayersIcon class="size-4 text-muted-foreground" />
					<h1 class="text-sm font-semibold">{m.podman_kube_title()}</h1>
				</div>
			</div>

			{#if podmanEnvs.length > 0}
				<div class="flex items-center gap-2">
					<EnvironmentDestinationSelect
						bind:value={() => environmentId, (value) => (picked = value)}
						permission="podman:kube"
						environments={podmanEnvs}
					/>
					<ButtonGroup.Root>
						<ArcaneButton
							action="deploy"
							tone="ghost"
							class={`${templateBtnClass} gap-2 rounded-r-none`}
							customLabel={m.podman_kube_play()}
							onclick={handlePlay}
							loading={loading.play}
							disabled={loading.play || loading.down || !yaml.trim()}
						/>
						<DropdownMenu.Root>
							<DropdownMenu.Trigger>
								{#snippet child({ props })}
									<ArcaneButton
										{...props}
										action="base"
										tone="ghost"
										class={`${templateBtnClass} -ml-px rounded-l-none px-2`}
										icon={ArrowDownIcon}
									/>
								{/snippet}
							</DropdownMenu.Trigger>
							<DropdownMenu.Content align="end" class="min-w-55">
								<DropdownMenu.Group>
									<DropdownMenu.Item disabled={loading.play || loading.down} onclick={() => (generateOpen = true)}>
										<TerminalIcon class="size-4" />
										{m.podman_kube_generate()}
									</DropdownMenu.Item>
									<DropdownMenu.Separator />
									<DropdownMenu.Item disabled={loading.play || loading.down || !yaml.trim()} onclick={handleDown}>
										<TrashIcon class="size-4" />
										{m.common_down()}
									</DropdownMenu.Item>
								</DropdownMenu.Group>
							</DropdownMenu.Content>
						</DropdownMenu.Root>
					</ButtonGroup.Root>
				</div>
			{/if}
		</div>
	</div>

	{#if podmanEnvs.length === 0}
		<div class="mx-auto w-full max-w-full px-6 py-6">
			<EmptyState icon={LayersIcon} title={m.common_unavailable()} description={m.podman_environments_unavailable()} />
		</div>
	{:else}
		<div class="flex min-h-0 flex-1 flex-col gap-3">
			<div class="flex shrink-0 flex-wrap items-center gap-x-6 gap-y-3">
				<div class="flex items-center gap-2">
					<Checkbox id="kube-start" checked={start} onCheckedChange={(checked) => (start = checked === true)} />
					<Label for="kube-start" weight="normal">{m.podman_kube_start()}</Label>
				</div>
				<div class="flex items-center gap-2">
					<Checkbox id="kube-replace" checked={replace} onCheckedChange={(checked) => (replace = checked === true)} />
					<Label for="kube-replace" weight="normal">{m.podman_kube_replace()}</Label>
				</div>
				<div class="flex min-w-72 items-center gap-2">
					<Label for="kube-networks" weight="normal" class="shrink-0">{m.resource_networks_cap()}</Label>
					{@render multiSelect('kube-networks', networkItems, networks, networksQuery.isLoading, (value) => (networks = value))}
				</div>
			</div>

			{#if resultLines.length > 0}
				<ul class="shrink-0 space-y-1 font-mono text-xs text-muted-foreground">
					{#each withOccurrenceKeys(resultLines) as line (line.key)}
						<li>{line.value}</li>
					{/each}
				</ul>
			{/if}

			<div class="flex min-h-0 flex-1 flex-col">
				<CodePanel
					open={true}
					title={m.podman_kube_yaml()}
					language="yaml"
					validationMode="none"
					bind:value={yaml}
					fileId="podman:kube:yaml"
				/>
			</div>
		</div>
	{/if}
</div>

<ResponsiveDialog
	bind:open={generateOpen}
	title={m.podman_kube_generate()}
	description={m.podman_kube_generate_description()}
	contentClass="sm:max-w-lg"
>
	<div class="space-y-4">
		<div class="space-y-2">
			<Label for="kube-generate-names">{m.podman_kube_generate_names()}</Label>
			{@render multiSelect(
				'kube-generate-names',
				generateSourcesQuery.data ?? [],
				generateNames,
				generateSourcesQuery.isLoading,
				(value) => (generateNames = value)
			)}
		</div>
		<SelectWithLabel
			id="kube-generate-type"
			label={m.common_type()}
			bind:value={() => generateType, (value) => (generateType = value as PodmanKubeGenerateType)}
			options={PODMAN_KUBE_GENERATE_TYPES.map((type) => ({ label: type, value: type }))}
		/>
		<LabeledSwitch id="kube-generate-service" label={m.podman_kube_generate_service()} bind:checked={generateService} />
	</div>
	{#snippet footer()}
		<ArcaneButton
			action="create"
			customLabel={m.podman_kube_generate()}
			onclick={handleGenerate}
			loading={loading.generate}
			disabled={loading.generate || generateNames.length === 0}
		/>
	{/snippet}
</ResponsiveDialog>
