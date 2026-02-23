<script lang="ts">
	import * as Card from '$lib/components/ui/card';
	import { ArcaneButton } from '$lib/components/arcane-button/index.js';
	import { Input } from '$lib/components/ui/input/index.js';
	import { Textarea } from '$lib/components/ui/textarea/index.js';
	import { CopyButton } from '$lib/components/ui/copy-button';
	import { useEnvironmentRefresh } from '$lib/hooks/use-environment-refresh.svelte';
	import { LockIcon, TrashIcon } from '$lib/icons';
	import { ResourcePageLayout, type ActionButton, type StatCardConfig } from '$lib/layouts/index.js';
	import { m } from '$lib/paraglide/messages';
	import { swarmService } from '$lib/services/swarm-service';
	import userStore from '$lib/stores/user-store';
	import type { SwarmSecretSummary } from '$lib/types/swarm.type';
	import { handleApiResultWithCallbacks } from '$lib/utils/api.util';
	import { tryCatch } from '$lib/utils/try-catch';
	import { fromStore } from 'svelte/store';
	import { toast } from 'svelte-sonner';
	import { formatDistanceToNow } from 'date-fns';
	import { openConfirmDialog } from '$lib/components/confirm-dialog';

	const storeUser = fromStore(userStore);
	const isAdmin = $derived(!!storeUser.current?.roles?.includes('admin'));

	let secrets = $state<SwarmSecretSummary[]>([]);
	let selectedSecretId = $state('');
	let createName = $state('');
	let createData = $state('');
	let editName = $state('');
	let editData = $state('');
	let isLoading = $state({
		refresh: false,
		create: false,
		update: false,
		delete: false
	});

	const selectedSecret = $derived(secrets.find((secret) => secret.id === selectedSecretId) ?? null);

	function getSpecName(spec: Record<string, unknown> | null | undefined, fallback: string): string {
		const name = spec && typeof spec === 'object' ? (spec as Record<string, unknown>).Name : undefined;
		return typeof name === 'string' && name.trim() ? name : fallback.slice(0, 12);
	}

	function getVersionIndex(version?: { index?: number; Index?: number }): number | undefined {
		return version?.index ?? version?.Index;
	}

	function decodeBase64ToText(base64Value: string): string {
		try {
			const binary = atob(base64Value);
			const bytes = Uint8Array.from(binary, (char) => char.charCodeAt(0));
			return new TextDecoder().decode(bytes);
		} catch {
			return '';
		}
	}

	function encodeTextToBase64(value: string): string {
		const bytes = new TextEncoder().encode(value);
		let binary = '';
		for (const byte of bytes) {
			binary += String.fromCharCode(byte);
		}
		return btoa(binary);
	}

	function formatTimestamp(timestamp: string): string {
		if (!timestamp) return m.common_unknown();
		return formatDistanceToNow(new Date(timestamp), { addSuffix: true });
	}

	function selectSecret(secret: SwarmSecretSummary) {
		selectedSecretId = secret.id;
		const spec = (secret.spec ?? {}) as Record<string, unknown>;
		editName = typeof spec.Name === 'string' ? spec.Name : '';
		editData = typeof spec.Data === 'string' ? decodeBase64ToText(spec.Data) : '';
	}

	function clearSelectedSecret() {
		selectedSecretId = '';
		editName = '';
		editData = '';
	}

	function toggleSecret(secret: SwarmSecretSummary) {
		if (selectedSecretId === secret.id) {
			clearSelectedSecret();
			return;
		}

		selectSecret(secret);
	}

	async function refresh() {
		isLoading.refresh = true;
		try {
			secrets = await swarmService.getSecrets();
			if (selectedSecretId && !secrets.some((secret) => secret.id === selectedSecretId)) {
				clearSelectedSecret();
			}
		} finally {
			isLoading.refresh = false;
		}
	}

	useEnvironmentRefresh(refresh);

	$effect(() => {
		refresh();
	});

	async function createSecret() {
		const name = createName.trim();
		if (!name) {
			toast.error(m.swarm_secrets_name_required());
			return;
		}
		const spec: Record<string, unknown> = {
			Name: name,
			Data: encodeTextToBase64(createData)
		};

		handleApiResultWithCallbacks({
			result: await tryCatch(swarmService.createSecret({ spec })),
			message: m.swarm_secrets_create_failed(),
			setLoadingState: (value) => (isLoading.create = value),
			onSuccess: async (created) => {
				toast.success(m.swarm_secrets_create_success({ name: getSpecName(created.spec, created.id) }));
				createName = '';
				createData = '';
				await refresh();
				selectSecret(created);
			}
		});
	}

	async function updateSecret() {
		if (!selectedSecret) return;
		const name = editName.trim();
		if (!name) {
			toast.error(m.swarm_secrets_name_required());
			return;
		}

		const version = getVersionIndex(selectedSecret.version);
		const request: { version?: number; spec: Record<string, unknown> } = {
			spec: {
				...(selectedSecret.spec ?? {}),
				Name: name,
				Data: encodeTextToBase64(editData)
			}
		};
		if (version !== undefined) request.version = version;

		handleApiResultWithCallbacks({
			result: await tryCatch(swarmService.updateSecret(selectedSecret.id, request)),
			message: m.swarm_secrets_update_failed({ name: getSpecName(selectedSecret.spec, selectedSecret.id) }),
			setLoadingState: (value) => (isLoading.update = value),
			onSuccess: async () => {
				toast.success(m.swarm_secrets_update_success({ name: getSpecName(selectedSecret.spec, selectedSecret.id) }));
				await refresh();
			}
		});
	}

	function removeSecret(secret: SwarmSecretSummary) {
		const name = getSpecName(secret.spec, secret.id);
		openConfirmDialog({
			title: m.common_delete_title({ resource: m.swarm_secret() }),
			message: m.swarm_secrets_delete_confirm({ name }),
			confirm: {
				label: m.common_delete(),
				destructive: true,
				action: async () => {
					handleApiResultWithCallbacks({
						result: await tryCatch(swarmService.removeSecret(secret.id)),
						message: m.swarm_secrets_delete_failed({ name }),
						setLoadingState: (value) => (isLoading.delete = value),
						onSuccess: async () => {
							toast.success(m.swarm_secrets_delete_success({ name }));
							await refresh();
						}
					});
				}
			}
		});
	}

	const actionButtons: ActionButton[] = $derived([
		{
			id: 'refresh',
			action: 'restart',
			label: m.common_refresh(),
			onclick: refresh,
			loading: isLoading.refresh,
			disabled: isLoading.refresh
		}
	]);

	const statCards: StatCardConfig[] = $derived([
		{
			title: m.swarm_secrets_title(),
			value: secrets.length,
			icon: LockIcon,
			iconColor: 'text-blue-500'
		}
	]);
</script>

<ResourcePageLayout
	title={m.swarm_secrets_title()}
	subtitle={m.swarm_secrets_subtitle()}
	icon={LockIcon}
	{actionButtons}
	{statCards}
>
	{#snippet mainContent()}
		<div class="grid gap-4 lg:grid-cols-[1fr,1.1fr]">
			<Card.Root class="pt-0">
				<Card.Header>
					<Card.Title>{m.swarm_secrets_create_title()}</Card.Title>
					<Card.Description>{m.swarm_secrets_create_subtitle()}</Card.Description>
				</Card.Header>
				<Card.Content class="space-y-3 pb-6">
					<Input placeholder={m.swarm_secrets_name_placeholder()} bind:value={createName} />
					<Textarea
						rows={10}
						bind:value={createData}
						placeholder={m.swarm_secrets_data_placeholder()}
						class="font-mono text-xs"
					/>
					<ArcaneButton
						action="create"
						customLabel={m.swarm_secrets_create_button()}
						onclick={createSecret}
						disabled={!isAdmin || isLoading.create}
						loading={isLoading.create}
					/>
				</Card.Content>
			</Card.Root>

			<div class="space-y-4">
				<Card.Root class="pt-0">
					<Card.Header>
						<Card.Title>{m.swarm_secrets_list_title()}</Card.Title>
						<Card.Description>{m.swarm_secrets_list_subtitle()}</Card.Description>
					</Card.Header>
					<Card.Content class="space-y-3 pb-6">
						{#if secrets.length === 0}
							<div class="text-muted-foreground py-8 text-center text-sm">{m.swarm_secrets_empty()}</div>
						{:else}
							{#each secrets as secret (secret.id)}
								<Card.Root class="overflow-hidden border py-0">
									<button
										type="button"
										class={`w-full px-4 py-3 text-left transition-colors ${selectedSecretId === secret.id ? 'bg-muted/50' : 'hover:bg-muted/40'}`}
										onclick={() => toggleSecret(secret)}
									>
										<div class="flex items-center justify-between gap-2">
											<div class="min-w-0">
												<div class="truncate font-medium">{getSpecName(secret.spec, secret.id)}</div>
												<div class="text-muted-foreground font-mono text-xs">{secret.id}</div>
											</div>
											<div class="text-muted-foreground shrink-0 text-xs">{formatTimestamp(secret.updatedAt)}</div>
										</div>
									</button>

									{#if selectedSecretId === secret.id}
										<div class="space-y-3 border-t px-4 pt-4 pb-5">
											<div class="flex items-center gap-2">
												<div class="text-muted-foreground font-mono text-xs">{secret.id}</div>
												<CopyButton text={secret.id} />
											</div>
											<Input placeholder={m.swarm_secrets_name_placeholder()} bind:value={editName} />
											<Textarea
												rows={12}
												bind:value={editData}
												placeholder={m.swarm_secrets_data_placeholder()}
												class="font-mono text-xs"
											/>
											<div class="flex flex-wrap items-center gap-2 pt-1">
												<ArcaneButton
													action="save"
													customLabel={m.swarm_secrets_update_button()}
													onclick={updateSecret}
													disabled={!isAdmin || isLoading.update}
													loading={isLoading.update}
												/>
												<ArcaneButton
													action="remove"
													customLabel={m.swarm_secrets_delete_button()}
													icon={TrashIcon}
													onclick={() => removeSecret(secret)}
													disabled={!isAdmin || isLoading.delete}
													loading={isLoading.delete}
												/>
											</div>
										</div>
									{/if}
								</Card.Root>
							{/each}
						{/if}
					</Card.Content>
				</Card.Root>
			</div>
		</div>
	{/snippet}
</ResourcePageLayout>
