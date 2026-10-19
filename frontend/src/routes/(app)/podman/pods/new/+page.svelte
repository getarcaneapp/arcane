<script lang="ts">
	import { goto } from '$app/navigation';
	import { useQueryClient } from '@tanstack/svelte-query';
	import { toast } from 'svelte-sonner';

	import { ArcaneButton } from '#lib/components/arcane-button/index.js';
	import CodePanel from '#lib/components/code-panel.svelte';
	import KeyValueEditor, { type KeyValueRow } from '#lib/components/form/key-value-editor.svelte';
	import LabeledSwitch from '#lib/components/form/labeled-switch.svelte';
	import PortMappingEditor, { type PortMappingRow } from '#lib/components/form/port-mapping-editor.svelte';
	import TextInputWithLabel from '#lib/components/form/text-input-with-label.svelte';
	import * as Card from '#lib/components/ui/card/index.js';
	import { ArrowLeftIcon } from '#lib/icons/index.js';
	import { m } from '#lib/paraglide/messages.js';
	import { queryKeys } from '#lib/query/query-keys.js';
	import { podmanService } from '#lib/services/podman-service.js';
	import { environmentStore } from '#lib/stores/environment.store.svelte.js';
	import type { PodmanPodCreateRequest } from '#lib/types/podman.js';
	import { handleApiResultWithCallbacks } from '#lib/utils/api.js';
	import { tryCatch } from '#lib/utils/try-catch.js';

	const queryClient = useQueryClient();

	let name = $state('');
	let hostname = $state('');
	let network = $state('');
	let sharedNamespaces = $state('');
	let noInfra = $state(false);
	let labels = $state<KeyValueRow[]>([]);
	let ports = $state<PortMappingRow[]>([]);
	let advanced = $state('');
	let submitting = $state(false);

	function parseAdvanced(): Record<string, unknown> | null | undefined {
		if (!advanced.trim()) return undefined;
		const parsed: unknown = JSON.parse(advanced);
		return typeof parsed === 'object' && parsed !== null && !Array.isArray(parsed) ? (parsed as Record<string, unknown>) : null;
	}

	async function handleSubmit() {
		if (!name.trim()) {
			toast.error(m.common_field_required({ field: m.common_name() }));
			return;
		}
		const advancedResult = await tryCatch(Promise.resolve().then(parseAdvanced));
		if (advancedResult.error !== null || advancedResult.data === null) {
			toast.error(m.podman_pod_advanced_invalid());
			return;
		}
		const labelEntries = labels.filter((row) => row.key.trim()).map((row) => [row.key.trim(), row.value]);
		const namespaces = sharedNamespaces
			.split(',')
			.map((entry) => entry.trim())
			.filter(Boolean);
		const portMappings = ports
			.filter((row) => row.containerPort.trim())
			.map((row) => ({
				containerPort: Number(row.containerPort),
				hostPort: row.hostPort.trim() ? Number(row.hostPort) : undefined,
				hostIp: row.hostIp.trim() || undefined,
				protocol: row.protocol
			}));
		const request: PodmanPodCreateRequest = {
			name: name.trim(),
			hostname: hostname.trim() || undefined,
			network: network.trim() || undefined,
			noInfra: noInfra || undefined,
			labels: labelEntries.length > 0 ? Object.fromEntries(labelEntries) : undefined,
			sharedNamespaces: namespaces.length > 0 ? namespaces : undefined,
			portMappings: portMappings.length > 0 ? portMappings : undefined,
			advanced: advancedResult.data
		};
		submitting = true;
		await handleApiResultWithCallbacks({
			result: await tryCatch(podmanService.createPod(request)),
			message: m.common_create_failed({ resource: request.name }),
			setLoadingState: (value) => (submitting = value),
			onSuccess: async (created) => {
				toast.success(m.common_create_success({ resource: request.name }));
				await queryClient.invalidateQueries({
					queryKey: queryKeys.podman.pods(environmentStore.selected?.id ?? '0')
				});
				await goto(`/podman/pods/${encodeURIComponent(created.id)}`);
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
				href="/podman/pods"
				class="gap-2 bg-transparent"
				icon={ArrowLeftIcon}
				customLabel={m.common_back()}
			/>
			<div class="hidden h-4 w-px bg-border sm:block"></div>
			<h1 class="flex-1 text-base font-semibold">{m.common_create_button({ resource: m.podman_pod() })}</h1>
			<ArcaneButton action="create" onclick={handleSubmit} loading={submitting} disabled={submitting} />
		</div>
	</div>

	<div class="mx-auto w-full max-w-full space-y-6 px-6 py-6">
		<Card.Root>
			<Card.Header>
				<Card.Title>{m.common_details_title({ resource: m.podman_pod() })}</Card.Title>
				<Card.Description>{m.podman_pod_create_description()}</Card.Description>
			</Card.Header>
			<Card.Content class="grid gap-4 md:grid-cols-2">
				<TextInputWithLabel id="pod-name" label={m.common_name()} bind:value={name} required disabled={submitting} />
				<TextInputWithLabel id="pod-hostname" label={m.hostname()} bind:value={hostname} disabled={submitting} />
				<TextInputWithLabel id="pod-network" label={m.resource_network_cap()} bind:value={network} disabled={submitting} />
				<TextInputWithLabel
					id="pod-namespaces"
					label={m.podman_pod_shared_namespaces()}
					description={m.podman_pod_shared_namespaces_description()}
					placeholder="ipc,net,uts"
					bind:value={sharedNamespaces}
					disabled={submitting}
				/>
				<LabeledSwitch
					id="pod-no-infra"
					label={m.podman_pod_no_infra()}
					description={m.podman_pod_no_infra_description()}
					bind:checked={noInfra}
					disabled={submitting}
				/>
			</Card.Content>
		</Card.Root>

		<Card.Root>
			<Card.Header>
				<Card.Title>{m.common_port_mappings()}</Card.Title>
			</Card.Header>
			<Card.Content>
				<PortMappingEditor bind:rows={ports} disabled={submitting} />
			</Card.Content>
		</Card.Root>

		<Card.Root>
			<Card.Header>
				<Card.Title>{m.common_labels()}</Card.Title>
			</Card.Header>
			<Card.Content>
				<KeyValueEditor bind:rows={labels} disabled={submitting} />
			</Card.Content>
		</Card.Root>

		<CodePanel title={m.common_advanced()} language="json" bind:value={advanced} autoHeight />
	</div>
</div>
