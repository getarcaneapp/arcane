<script lang="ts">
	import { useQueryClient } from '@tanstack/svelte-query';
	import { toast } from 'svelte-sonner';

	import { ArcaneButton } from '#lib/components/arcane-button/index.js';
	import CodePanel from '#lib/components/code-panel.svelte';
	import LabeledSwitch from '#lib/components/form/labeled-switch.svelte';
	import SelectWithLabel from '#lib/components/form/select-with-label.svelte';
	import TextInputWithLabel from '#lib/components/form/text-input-with-label.svelte';
	import * as Card from '#lib/components/ui/card/index.js';
	import { LayersIcon } from '#lib/icons/index.js';
	import { ResourcePageLayout } from '#lib/layouts/index.js';
	import { m } from '#lib/paraglide/messages.js';
	import { queryKeys } from '#lib/query/query-keys.js';
	import { podmanService } from '#lib/services/podman-service.js';
	import { environmentStore } from '#lib/stores/environment.store.svelte.js';
	import { PODMAN_KUBE_GENERATE_TYPES, type PodmanKubeGenerateType } from '#lib/types/podman.js';
	import { handleApiResultWithCallbacks } from '#lib/utils/api.js';
	import { parseList } from '#lib/utils/form-parsers.js';
	import { withOccurrenceKeys } from '#lib/utils/formatting.js';
	import { tryCatch } from '#lib/utils/try-catch.js';

	const queryClient = useQueryClient();

	let yaml = $state('');
	let start = $state(true);
	let replace = $state(false);
	let networks = $state('');
	let force = $state(false);
	let generateNames = $state('');
	let generateType = $state<PodmanKubeGenerateType>('pod');
	let generateService = $state(false);
	let generated = $state('');
	let resultLines = $state<string[]>([]);
	let loading = $state({ play: false, down: false, generate: false });

	async function invalidatePods() {
		await queryClient.invalidateQueries({ queryKey: queryKeys.podman.environment(environmentStore.selected?.id ?? '0') });
	}

	async function handlePlay() {
		if (!yaml.trim()) {
			toast.error(m.common_field_required({ field: m.podman_kube_yaml() }));
			return;
		}
		const networkList = parseList(networks);
		loading.play = true;
		await handleApiResultWithCallbacks({
			result: await tryCatch(
				podmanService.kubePlay({ yaml, start, replace, networks: networkList.length > 0 ? networkList : undefined })
			),
			message: m.podman_kube_play_failed(),
			setLoadingState: (value) => (loading.play = value),
			onSuccess: async (report) => {
				resultLines = [
					...(report.pods ?? []).flatMap((pod) => [
						m.podman_kube_pod_created({ id: pod.id.slice(0, 12), count: pod.containers?.length ?? 0 }),
						...(pod.containerErrors ?? [])
					]),
					...(report.volumes ?? []).map((volume) => volume.name)
				];
				toast.success(m.podman_kube_play_success({ count: report.pods?.length ?? 0 }));
				await invalidatePods();
			}
		});
	}

	async function handleDown() {
		if (!yaml.trim()) {
			toast.error(m.common_field_required({ field: m.podman_kube_yaml() }));
			return;
		}
		loading.down = true;
		await handleApiResultWithCallbacks({
			result: await tryCatch(podmanService.kubeDown({ yaml, force })),
			message: m.podman_kube_down_failed(),
			setLoadingState: (value) => (loading.down = value),
			onSuccess: async (report) => {
				resultLines = (report.rmReport ?? []).map((pod) => m.podman_kube_pod_removed({ id: pod.id.slice(0, 12) }));
				toast.success(m.podman_kube_down_success({ count: report.rmReport?.length ?? 0 }));
				await invalidatePods();
			}
		});
	}

	async function handleGenerate() {
		const names = parseList(generateNames);
		if (names.length === 0) {
			toast.error(m.common_field_required({ field: m.podman_kube_generate_names() }));
			return;
		}
		loading.generate = true;
		await handleApiResultWithCallbacks({
			result: await tryCatch(podmanService.kubeGenerate({ names, type: generateType, service: generateService || undefined })),
			message: m.podman_kube_generate_failed(),
			setLoadingState: (value) => (loading.generate = value),
			onSuccess: (response) => {
				generated = response.yaml;
			}
		});
	}
</script>

<ResourcePageLayout title={m.podman_kube_title()} subtitle={m.podman_kube_subtitle()} icon={LayersIcon} environmentPicker>
	{#snippet mainContent()}
		<div class="grid gap-4 xl:grid-cols-2">
			<div class="space-y-4">
				<CodePanel title={m.podman_kube_yaml()} language="yaml" validationMode="none" bind:value={yaml} autoHeight />
				<Card.Root>
					<Card.Header>
						<Card.Title>{m.podman_kube_play()}</Card.Title>
						<Card.Description>{m.podman_kube_play_description()}</Card.Description>
					</Card.Header>
					<Card.Content>
						<div class="space-y-4">
							<LabeledSwitch id="kube-start" label={m.podman_kube_start()} bind:checked={start} />
							<LabeledSwitch id="kube-replace" label={m.podman_kube_replace()} bind:checked={replace} />
							<TextInputWithLabel
								id="kube-networks"
								label={m.resource_networks_cap()}
								description={m.podman_comma_separated()}
								bind:value={networks}
							/>
							<LabeledSwitch
								id="kube-force"
								label={m.podman_kube_force()}
								description={m.podman_kube_force_description()}
								bind:checked={force}
							/>
							<div class="flex flex-wrap gap-2">
								<ArcaneButton
									action="deploy"
									customLabel={m.podman_kube_play()}
									onclick={handlePlay}
									loading={loading.play}
									disabled={loading.play || loading.down}
								/>
								<ArcaneButton
									action="remove"
									customLabel={m.common_down()}
									onclick={handleDown}
									loading={loading.down}
									disabled={loading.play || loading.down}
								/>
							</div>
							{#if resultLines.length > 0}
								<ul class="space-y-1 rounded-lg border p-3 font-mono text-xs">
									{#each withOccurrenceKeys(resultLines) as line (line.key)}
										<li>{line.value}</li>
									{/each}
								</ul>
							{/if}
						</div>
					</Card.Content>
				</Card.Root>
			</div>

			<div class="space-y-4">
				<Card.Root>
					<Card.Header>
						<Card.Title>{m.podman_kube_generate()}</Card.Title>
						<Card.Description>{m.podman_kube_generate_description()}</Card.Description>
					</Card.Header>
					<Card.Content>
						<div class="space-y-4">
							<TextInputWithLabel
								id="kube-generate-names"
								label={m.podman_kube_generate_names()}
								description={m.podman_comma_separated()}
								bind:value={generateNames}
							/>
							<SelectWithLabel
								id="kube-generate-type"
								label={m.common_type()}
								bind:value={() => generateType, (value) => (generateType = value as PodmanKubeGenerateType)}
								options={PODMAN_KUBE_GENERATE_TYPES.map((type) => ({ label: type, value: type }))}
							/>
							<LabeledSwitch id="kube-generate-service" label={m.podman_kube_generate_service()} bind:checked={generateService} />
							<div class="flex flex-wrap gap-2">
								<ArcaneButton
									action="create"
									customLabel={m.podman_kube_generate()}
									onclick={handleGenerate}
									loading={loading.generate}
									disabled={loading.generate}
								/>
								{#if generated}
									<ArcaneButton
										action="base"
										tone="outline"
										customLabel={m.podman_kube_use_generated()}
										onclick={() => (yaml = generated)}
									/>
								{/if}
							</div>
						</div>
					</Card.Content>
				</Card.Root>
				{#if generated}
					<CodePanel
						title={m.podman_kube_generated()}
						language="yaml"
						validationMode="none"
						value={generated}
						readOnly
						autoHeight
					/>
				{/if}
			</div>
		</div>
	{/snippet}
</ResourcePageLayout>
