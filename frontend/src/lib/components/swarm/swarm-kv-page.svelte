<script lang="ts">
	import { ArcaneButton } from '$lib/components/arcane-button/index.js';
	import { openConfirmDialog } from '$lib/components/confirm-dialog';
	import IfPermitted from '$lib/components/if-permitted.svelte';
	import { CopyButton } from '$lib/components/ui/copy-button';
	import * as Card from '$lib/components/ui/card';
	import { Input } from '$lib/components/ui/input/index.js';
	import { Textarea } from '$lib/components/ui/textarea/index.js';
	import { useEnvironmentRefresh } from '$lib/hooks/use-environment-refresh.svelte';
	import { TrashIcon } from '$lib/icons';
	import { ResourcePageLayout, type ActionButton, type StatCardConfig } from '$lib/layouts/index.js';
	import { m } from '$lib/paraglide/messages';
	import { handleApiResultWithCallbacks, tryCatch } from '$lib/utils/api';
	import { decodeBase64ToText, encodeTextToBase64, formatSwarmTimestamp, getSwarmSpecName } from '$lib/utils/swarm-kv';
	import { onMount, type Component } from 'svelte';
	import { toast } from 'svelte-sonner';

	export type SwarmKvItem = {
		id: string;
		spec?: Record<string, unknown> | null;
		updatedAt: string;
	};
	type LoadingState = {
		refresh: boolean;
		create: boolean;
		delete: boolean;
	};
	type SwarmKvMessages = {
		pageTitle: string;
		pageSubtitle: string;
		statTitle: string;
		createTitle: string;
		createSubtitle: string;
		namePlaceholder: string;
		dataPlaceholder: string;
		createButton: string;
		listTitle: string;
		listSubtitle: string;
		empty: string;
		immutableNotice: string;
		deleteButton: string;
		nameRequired: string;
		createFailed: string;
		createSuccess: (name: string) => string;
		deleteConfirm: (name: string) => string;
		deleteFailed: (name: string) => string;
		deleteSuccess: (name: string) => string;
	};

	let {
		icon,
		permission,
		resourceLabel,
		canManage,
		messages,
		loadItems,
		createItem,
		removeItem
	}: {
		icon: Component;
		permission: string;
		resourceLabel: string;
		canManage: boolean;
		messages: SwarmKvMessages;
		loadItems: () => Promise<SwarmKvItem[]>;
		createItem: (spec: Record<string, unknown>) => Promise<SwarmKvItem>;
		removeItem: (id: string) => Promise<unknown>;
	} = $props();

	let items = $state<SwarmKvItem[]>([]);
	let selectedId = $state('');
	let createName = $state('');
	let createData = $state('');
	let editName = $state('');
	let editData = $state('');
	let isLoading = $state<LoadingState>({
		refresh: false,
		create: false,
		delete: false
	});

	function selectItem(item: SwarmKvItem) {
		selectedId = item.id;
		const spec = (item.spec ?? {}) as Record<string, unknown>;
		editName = typeof spec['Name'] === 'string' ? spec['Name'] : '';
		editData = typeof spec['Data'] === 'string' ? decodeBase64ToText(spec['Data']) : '';
	}

	function clearSelectedItem() {
		selectedId = '';
		editName = '';
		editData = '';
	}

	function toggleItem(item: SwarmKvItem) {
		if (selectedId === item.id) {
			clearSelectedItem();
			return;
		}

		selectItem(item);
	}

	async function refresh() {
		isLoading.refresh = true;
		try {
			items = await loadItems();
			if (selectedId && !items.some((item) => item.id === selectedId)) {
				clearSelectedItem();
			}
		} finally {
			isLoading.refresh = false;
		}
	}

	useEnvironmentRefresh(refresh);

	onMount(() => {
		void refresh();
	});

	async function handleCreate() {
		const name = createName.trim();
		if (!name) {
			toast.error(messages.nameRequired);
			return;
		}
		const spec: Record<string, unknown> = {
			Name: name,
			Data: encodeTextToBase64(createData)
		};

		handleApiResultWithCallbacks({
			result: await tryCatch(createItem(spec)),
			message: messages.createFailed,
			setLoadingState: (value) => (isLoading.create = value),
			onSuccess: async (created) => {
				toast.success(messages.createSuccess(getSwarmSpecName(created.spec, created.id)));
				createName = '';
				createData = '';
				await refresh();
				selectItem(created);
			}
		});
	}

	function handleRemove(item: SwarmKvItem) {
		const name = getSwarmSpecName(item.spec, item.id);
		openConfirmDialog({
			title: m.common_delete_title({ resource: resourceLabel }),
			message: messages.deleteConfirm(name),
			confirm: {
				label: m.common_delete(),
				destructive: true,
				action: async () => {
					handleApiResultWithCallbacks({
						result: await tryCatch(removeItem(item.id)),
						message: messages.deleteFailed(name),
						setLoadingState: (value) => (isLoading.delete = value),
						onSuccess: async () => {
							toast.success(messages.deleteSuccess(name));
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
			title: messages.statTitle,
			value: items.length,
			icon,
			iconColor: 'text-blue-500'
		}
	]);
</script>

<ResourcePageLayout title={messages.pageTitle} subtitle={messages.pageSubtitle} {icon} {actionButtons} {statCards}>
	{#snippet mainContent()}
		<div class="grid gap-4 lg:grid-cols-[1fr,1.1fr]">
			<Card.Root class="pt-0">
				<Card.Header>
					<Card.Title>{messages.createTitle}</Card.Title>
					<Card.Description>{messages.createSubtitle}</Card.Description>
				</Card.Header>
				<Card.Content class="space-y-3 pb-6">
					<Input placeholder={messages.namePlaceholder} bind:value={createName} />
					<Textarea rows={10} bind:value={createData} placeholder={messages.dataPlaceholder} class="font-mono text-xs" />
					<IfPermitted perm={permission}>
						<ArcaneButton
							action="create"
							customLabel={messages.createButton}
							onclick={handleCreate}
							disabled={!canManage || isLoading.create}
							loading={isLoading.create}
						/>
					</IfPermitted>
				</Card.Content>
			</Card.Root>

			<div class="space-y-4">
				<Card.Root class="pt-0">
					<Card.Header>
						<Card.Title>{messages.listTitle}</Card.Title>
						<Card.Description>{messages.listSubtitle}</Card.Description>
					</Card.Header>
					<Card.Content class="space-y-3 pb-6">
						{#if items.length === 0}
							<div class="text-muted-foreground py-8 text-center text-sm">{messages.empty}</div>
						{:else}
							{#each items as item (item.id)}
								<Card.Root class="overflow-hidden border py-0">
									<button
										type="button"
										class={`w-full px-4 py-3 text-left transition-colors ${selectedId === item.id ? 'bg-muted/50' : 'hover:bg-muted/40'}`}
										onclick={() => toggleItem(item)}
									>
										<div class="flex items-center justify-between gap-2">
											<div class="min-w-0">
												<div class="truncate font-medium">{getSwarmSpecName(item.spec, item.id)}</div>
												<div class="text-muted-foreground font-mono text-xs">{item.id}</div>
											</div>
											<div class="text-muted-foreground shrink-0 text-xs">{formatSwarmTimestamp(item.updatedAt)}</div>
										</div>
									</button>

									{#if selectedId === item.id}
										<div class="space-y-3 border-t px-4 pt-4 pb-5">
											<div class="flex items-center gap-2">
												<div class="text-muted-foreground font-mono text-xs">{item.id}</div>
												<CopyButton text={item.id} />
											</div>
											<p class="text-muted-foreground text-sm">{messages.immutableNotice}</p>
											<Input placeholder={messages.namePlaceholder} bind:value={editName} readonly />
											<Textarea
												rows={12}
												bind:value={editData}
												placeholder={messages.dataPlaceholder}
												class="font-mono text-xs"
												readonly
											/>
											<div class="flex flex-wrap items-center gap-2 pt-1">
												<ArcaneButton
													action="remove"
													customLabel={messages.deleteButton}
													icon={TrashIcon}
													onclick={() => handleRemove(item)}
													disabled={!canManage || isLoading.delete}
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
