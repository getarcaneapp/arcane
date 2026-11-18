<script lang="ts">
	import { createQuery } from '@tanstack/svelte-query';

	import { ArcaneButton } from '#lib/components/arcane-button/index.js';
	import SheetFooterActions from '#lib/components/sheets/sheet-footer-actions.svelte';
	import * as Alert from '#lib/components/ui/alert/index.js';
	import { Badge } from '#lib/components/ui/badge/index.js';
	import { Label } from '#lib/components/ui/label/index.js';
	import * as ResponsiveDialog from '#lib/components/ui/responsive-dialog/index.js';
	import * as Select from '#lib/components/ui/select/index.js';
	import { AlertIcon, RefreshIcon } from '#lib/icons/index.js';
	import { m } from '#lib/paraglide/messages.js';
	import { queryKeys } from '#lib/query/query-keys.js';
	import { secretSourceService } from '#lib/services/secret-source-service.js';
	import type { BindingTarget, BitwardenTarget, InfisicalTarget, SecretProvider } from '#lib/types/secret-source.js';
	import { extractApiErrorMessage } from '#lib/utils/api.js';
	import { tryCatch } from '#lib/utils/try-catch.js';

	import type { ContainerSecretFill } from '../containers/container-form/container-form-state.js';
	import BitwardenTargetFields from './bitwarden-target-fields.svelte';
	import InfisicalTargetFields from './infisical-target-fields.svelte';
	import ProviderIcon from './provider-icon.svelte';
	import ProviderTargetFields from './provider-target-fields.svelte';
	import {
		describeGenericTarget,
		isGenericProvider,
		isGenericTargetComplete,
		pickGenericTarget,
		providerLabel
	} from './providers.js';

	let {
		open = $bindable(false),
		onAdd
	}: {
		open: boolean;
		// Receives the chosen target; values are read on the server at create.
		onAdd: (fill: ContainerSecretFill) => void;
	} = $props();

	const sourcesQuery = createQuery(() => ({
		queryKey: queryKeys.secretSources.list(),
		queryFn: () => secretSourceService.list(),
		enabled: open
	}));
	const sources = $derived(sourcesQuery.data ?? []);

	let sourceId = $state('');
	let infisicalTarget = $state<InfisicalTarget>({
		projectId: '',
		environment: '',
		secretPath: '/',
		includeImports: true,
		expandReferences: true
	});
	let bitwardenTarget = $state<BitwardenTarget>({ scope: 'folder', id: '', name: '' });
	let genericTarget = $state<BindingTarget>({});

	const source = $derived(sources.find((candidate) => candidate.id === sourceId));
	const provider = $derived<SecretProvider | undefined>(source?.provider);
	const target = $derived<BindingTarget | null>(
		provider === 'infisical' && infisicalTarget.projectId && infisicalTarget.environment
			? { infisical: infisicalTarget }
			: provider === 'bitwarden' && bitwardenTarget.id
				? { bitwarden: bitwardenTarget }
				: isGenericProvider(provider) && isGenericTargetComplete(provider, genericTarget)
					? pickGenericTarget(provider, genericTarget)
					: null
	);

	let keys = $state<string[] | null>(null);
	let loadingKeys = $state(false);
	let error = $state<string | null>(null);

	async function loadKeys() {
		if (!target) return;
		loadingKeys = true;
		error = null;
		const result = await tryCatch(secretSourceService.targetKeys(sourceId, target));
		loadingKeys = false;
		if (result.error) {
			error = extractApiErrorMessage(result.error);
			return;
		}
		keys = result.data.keys;
	}

	function describe(current: BindingTarget): string {
		if (current.infisical) return `${current.infisical.environment} · ${current.infisical.secretPath}`;
		if (current.bitwarden) return current.bitwarden.name || current.bitwarden.id;
		return describeGenericTarget(current);
	}

	function add() {
		if (!target || !source || keys === null || keys.length === 0) return;
		onAdd({
			sourceId,
			target: structuredClone($state.snapshot(target)),
			sourceName: source.name,
			provider: source.provider,
			description: describe(target),
			keys
		});
		open = false;
	}
</script>

<ResponsiveDialog.Root
	bind:open
	variant="sheet"
	title={m.container_secrets_title()}
	description={m.container_secrets_description()}
	contentClass="sm:max-w-2xl"
>
	{#snippet children()}
		<div class="grid gap-4 py-6">
			{#if sourcesQuery.isSuccess && sources.length === 0}
				<Alert.Root variant="info" icon={AlertIcon} description={m.project_secrets_no_sources()} />
			{:else}
				<div class="space-y-2">
					<Label for="container-secret-source">{m.project_secrets_source()}</Label>
					<Select.Root
						type="single"
						value={sourceId}
						onValueChange={(value) => {
							sourceId = value;
							genericTarget = {};
							keys = null;
						}}
					>
						<Select.Trigger id="container-secret-source" class="w-full">
							<span>{source?.name ?? m.project_secrets_select_source()}</span>
						</Select.Trigger>
						<Select.Content>
							{#each sources as candidate (candidate.id)}
								<Select.Item value={candidate.id}>
									<ProviderIcon provider={candidate.provider} class="size-6 rounded-md p-1" />
									<div class="flex flex-col">
										<span>{candidate.name}</span>
										<span class="text-xs text-muted-foreground">{providerLabel(candidate.provider)}</span>
									</div>
								</Select.Item>
							{/each}
						</Select.Content>
					</Select.Root>
				</div>

				{#if sourceId && provider === 'infisical'}
					{#key sourceId}
						<InfisicalTargetFields {sourceId} bind:target={infisicalTarget} />
					{/key}
				{:else if sourceId && provider === 'bitwarden'}
					{#key sourceId}
						<BitwardenTargetFields {sourceId} bind:target={bitwardenTarget} />
					{/key}
				{:else if sourceId && isGenericProvider(provider)}
					{#key sourceId}
						<ProviderTargetFields {sourceId} {provider} bind:target={genericTarget} />
					{/key}
				{/if}

				{#if sourceId}
					<div class="flex items-center gap-2 border-t border-border/50 pt-4">
						<ArcaneButton
							action="base"
							tone="outline-primary"
							icon={RefreshIcon}
							customLabel={keys === null ? m.new_project_secrets_load() : m.new_project_secrets_reload()}
							loading={loadingKeys}
							disabled={!target || loadingKeys}
							onclick={() => void loadKeys()}
						/>
						{#if keys !== null}
							<span class="text-sm text-muted-foreground"
								>{keys.length === 1
									? m.project_secrets_keys_count_one()
									: m.project_secrets_keys_count({ count: keys.length })}</span
							>
						{/if}
					</div>

					{#if keys !== null && keys.length > 0}
						<div class="flex flex-wrap gap-1.5">
							{#each keys as key (key)}
								<Badge variant="outline" size="sm" mono>{key}</Badge>
							{/each}
						</div>
					{/if}
				{/if}

				{#if error}
					<Alert.Root
						variant="destructive-subtle"
						icon={AlertIcon}
						heading={m.project_secrets_check_failed()}
						description={error}
					/>
				{/if}
			{/if}
		</div>
	{/snippet}

	{#snippet footer()}
		<SheetFooterActions
			bind:open
			submitAction="save"
			submitLabel={m.container_secrets_add()}
			submitDisabled={!target || keys === null || keys.length === 0}
			onSubmit={add}
		/>
	{/snippet}
</ResponsiveDialog.Root>
