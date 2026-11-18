<script lang="ts">
	import { createQuery } from '@tanstack/svelte-query';

	import * as Alert from '#lib/components/ui/alert/index.js';
	import { Button } from '#lib/components/ui/button/index.js';
	import { Input } from '#lib/components/ui/input/index.js';
	import { Label } from '#lib/components/ui/label/index.js';
	import * as RadioGroup from '#lib/components/ui/radio-group/index.js';
	import * as Select from '#lib/components/ui/select/index.js';
	import { Spinner } from '#lib/components/ui/spinner/index.js';
	import { AlertIcon, ApiKeyIcon, FolderOpenIcon } from '#lib/icons/index.js';
	import { m } from '#lib/paraglide/messages.js';
	import { queryKeys } from '#lib/query/query-keys.js';
	import { secretSourceService } from '#lib/services/secret-source-service.js';
	import type { BindingTarget, OnePasswordScope } from '#lib/types/secret-source.js';
	import { extractApiErrorMessage } from '#lib/utils/api.js';

	import { type GenericProvider, withDefaultTarget } from './providers.js';

	// Target fields for Vault/OpenBao, Doppler, 1Password, and HTTP sources.
	let {
		sourceId,
		provider,
		target = $bindable()
	}: {
		sourceId: string;
		provider: GenericProvider;
		target: BindingTarget;
	} = $props();

	$effect.pre(() => {
		const filled = withDefaultTarget(provider, target);
		if (JSON.stringify(filled) !== JSON.stringify(target)) target = filled;
	});

	// ---- Vault / OpenBao ----
	const mountsQuery = createQuery(() => ({
		queryKey: queryKeys.secretSources.browse(sourceId, 'mounts'),
		queryFn: () => secretSourceService.browse(sourceId, { kind: 'mounts' }),
		enabled: provider === 'vault' && !!sourceId,
		retry: false
	}));
	const mounts = $derived(mountsQuery.data ?? []);

	// Lists the entries next to the typed path: the folder it is in.
	const vaultPrefix = $derived.by(() => {
		const path = (target.vault?.path ?? '').replace(/^\/+/, '');
		const slash = path.lastIndexOf('/');
		return slash >= 0 ? path.slice(0, slash) : '';
	});
	const pathsQuery = createQuery(() => ({
		queryKey: queryKeys.secretSources.browse(
			sourceId,
			'paths',
			target.vault?.mount ?? '',
			String(target.vault?.kvVersion ?? 2),
			vaultPrefix
		),
		queryFn: () =>
			secretSourceService.browse(sourceId, {
				kind: 'paths',
				mount: target.vault?.mount,
				kvVersion: target.vault?.kvVersion,
				path: vaultPrefix
			}),
		enabled: provider === 'vault' && !!sourceId && !!target.vault?.mount,
		retry: false
	}));

	function pickMount(mount: string) {
		if (!target.vault) return;
		const detail = mounts.find((candidate) => candidate.id === mount)?.detail ?? '';
		target.vault = { mount, path: '', kvVersion: detail.endsWith('1') ? 1 : 2 };
	}

	function pickPath(id: string, isFolder: boolean) {
		if (!target.vault) return;
		target.vault.path = isFolder ? `${id}/` : id;
	}

	function leaveVaultFolder() {
		if (!target.vault) return;
		const parent = vaultPrefix.includes('/') ? vaultPrefix.slice(0, vaultPrefix.lastIndexOf('/') + 1) : '';
		target.vault.path = parent;
	}

	// ---- Doppler ----
	const dopplerProjectsQuery = createQuery(() => ({
		queryKey: queryKeys.secretSources.browse(sourceId, 'projects'),
		queryFn: () => secretSourceService.browse(sourceId, { kind: 'projects' }),
		enabled: provider === 'doppler' && !!sourceId,
		retry: false
	}));
	// A service token cannot list projects: it is scoped to one config, which
	// the empty target {} stands for.
	$effect(() => {
		if (provider !== 'doppler' || !dopplerProjectsQuery.isError || !target.doppler) return;
		if (target.doppler.project !== undefined || target.doppler.config !== undefined) target.doppler = {};
	});
	const dopplerConfigsQuery = createQuery(() => ({
		queryKey: queryKeys.secretSources.browse(sourceId, 'configs', target.doppler?.project ?? ''),
		queryFn: () => secretSourceService.browse(sourceId, { kind: 'configs', projectId: target.doppler?.project }),
		enabled: provider === 'doppler' && !!sourceId && !!target.doppler?.project,
		retry: false
	}));

	// ---- 1Password and Proton Pass (same target shape) ----
	const vaultKey = $derived<'onepassword' | 'protonpass'>(provider === 'protonpass' ? 'protonpass' : 'onepassword');
	const isVaultProvider = $derived(provider === 'onepassword' || provider === 'protonpass');
	const scopes = $derived<{ value: OnePasswordScope; label: () => string; description: () => string }[]>([
		{
			value: 'vault',
			label: m.onepassword_scope_vault,
			description: provider === 'protonpass' ? m.protonpass_scope_vault_description : m.onepassword_scope_vault_description
		},
		{
			value: 'item',
			label: m.onepassword_scope_item,
			description: provider === 'protonpass' ? m.protonpass_scope_item_description : m.onepassword_scope_item_description
		}
	]);
	const opVaultsQuery = createQuery(() => ({
		queryKey: queryKeys.secretSources.browse(sourceId, 'vaults'),
		queryFn: () => secretSourceService.browse(sourceId, { kind: 'vaults' }),
		enabled: isVaultProvider && !!sourceId,
		retry: false
	}));
	const opItemsQuery = createQuery(() => ({
		queryKey: queryKeys.secretSources.browse(sourceId, 'items', target[vaultKey]?.vaultId ?? ''),
		queryFn: () => secretSourceService.browse(sourceId, { kind: 'items', vaultId: target[vaultKey]?.vaultId }),
		enabled: isVaultProvider && !!sourceId && !!target[vaultKey]?.vaultId && target[vaultKey]?.scope === 'item',
		retry: false
	}));
</script>

{#snippet browseError(error: unknown)}
	<Alert.Root
		variant="destructive-subtle"
		icon={AlertIcon}
		heading={m.secret_sources_test_failed()}
		description={extractApiErrorMessage(error)}
	/>
{/snippet}

{#if provider === 'vault' && target.vault}
	<div class="flex flex-col gap-3 sm:flex-row">
		<div class="flex-1 space-y-2">
			<Label for="vault-mount">{m.vault_mount()}</Label>
			{#if mountsQuery.isSuccess && mounts.length > 0}
				<Select.Root type="single" value={target.vault.mount} onValueChange={pickMount}>
					<Select.Trigger id="vault-mount" class="w-full"><span class="font-mono">{target.vault.mount}</span></Select.Trigger>
					<Select.Content>
						{#each mounts as mount (mount.id)}
							<Select.Item value={mount.id}>
								<span class="font-mono">{mount.name}</span>
								<span class="ml-2 text-xs text-muted-foreground">{mount.detail}</span>
							</Select.Item>
						{/each}
					</Select.Content>
				</Select.Root>
			{:else}
				<Input id="vault-mount" mono placeholder="secret" bind:value={target.vault.mount} />
			{/if}
		</div>
		<div class="space-y-2">
			<Label for="vault-kv">{m.vault_kv_version()}</Label>
			<Select.Root
				type="single"
				value={String(target.vault.kvVersion)}
				onValueChange={(value) => target.vault && (target.vault.kvVersion = Number(value))}
			>
				<Select.Trigger id="vault-kv" class="w-28"><span>KV v{target.vault.kvVersion}</span></Select.Trigger>
				<Select.Content>
					<Select.Item value="2">KV v2</Select.Item>
					<Select.Item value="1">KV v1</Select.Item>
				</Select.Content>
			</Select.Root>
		</div>
	</div>

	<div class="space-y-2">
		<Label for="vault-path">{m.vault_secret_path()}</Label>
		<Input id="vault-path" mono placeholder="apps/immich" bind:value={target.vault.path} />
		<p class="text-xs text-muted-foreground">{m.vault_secret_path_description()}</p>
		<div class="flex flex-wrap items-center gap-1.5">
			{#if vaultPrefix}
				<Button type="button" size="sm" variant="ghost" class="h-7" onclick={leaveVaultFolder}
					><span class="font-mono">..</span></Button
				>
			{/if}
			{#if pathsQuery.isPending && target.vault.mount}
				<Spinner class="size-4" />
			{:else}
				{#each pathsQuery.data ?? [] as entry (entry.id)}
					<Button
						type="button"
						size="sm"
						variant={entry.id === target.vault.path ? 'default' : 'outline'}
						class="h-7"
						onclick={() => pickPath(entry.id, entry.detail === 'folder')}
					>
						{#if entry.detail === 'folder'}<FolderOpenIcon class="size-3.5" />{:else}<ApiKeyIcon class="size-3.5" />{/if}
						<span class="font-mono">{entry.name}</span>
					</Button>
				{/each}
			{/if}
		</div>
	</div>
{:else if provider === 'doppler' && target.doppler}
	{#if dopplerProjectsQuery.isPending}
		<div class="flex items-center gap-2 text-sm text-muted-foreground"><Spinner class="size-4" />{m.common_loading()}</div>
	{:else if dopplerProjectsQuery.isError}
		<!-- Service tokens are scoped to one config and cannot list projects. -->
		<Alert.Root variant="info" icon={AlertIcon} description={m.doppler_service_token_hint()} />
	{:else}
		<div class="grid gap-3 sm:grid-cols-2">
			<div class="space-y-2">
				<Label for="doppler-project">{m.doppler_project()}</Label>
				<Select.Root
					type="single"
					value={target.doppler.project ?? ''}
					onValueChange={(value) => (target.doppler = { project: value, config: '' })}
				>
					<Select.Trigger id="doppler-project" class="w-full">
						<span>{target.doppler.project || m.doppler_select_project()}</span>
					</Select.Trigger>
					<Select.Content>
						{#each dopplerProjectsQuery.data ?? [] as project (project.id)}
							<Select.Item value={project.id}>{project.name}</Select.Item>
						{/each}
					</Select.Content>
				</Select.Root>
			</div>
			<div class="space-y-2">
				<Label for="doppler-config">{m.doppler_config()}</Label>
				<Select.Root
					type="single"
					value={target.doppler.config ?? ''}
					disabled={!target.doppler.project}
					onValueChange={(value) => target.doppler && (target.doppler.config = value)}
				>
					<Select.Trigger id="doppler-config" class="w-full">
						<span>{target.doppler.config || m.doppler_select_config()}</span>
					</Select.Trigger>
					<Select.Content>
						{#each dopplerConfigsQuery.data ?? [] as config (config.id)}
							<Select.Item value={config.id}>
								{config.name}
								{#if config.detail}<span class="ml-2 text-xs text-muted-foreground">{config.detail}</span>{/if}
							</Select.Item>
						{/each}
					</Select.Content>
				</Select.Root>
			</div>
		</div>
		{#if dopplerConfigsQuery.isError}
			{@render browseError(dopplerConfigsQuery.error)}
		{/if}
	{/if}
{:else if (provider === 'onepassword' || provider === 'protonpass') && target[vaultKey]}
	<div class="space-y-2">
		<Label class="mb-0">{m.onepassword_scope()}</Label>
		<RadioGroup.Root
			class="mt-2"
			value={target[vaultKey].scope}
			onValueChange={(value) =>
				target[vaultKey] && (target[vaultKey] = { ...target[vaultKey]!, scope: value as OnePasswordScope, itemId: '', name: '' })}
		>
			<div class="grid gap-2">
				{#each scopes as scope (scope.value)}
					<label class="flex cursor-pointer items-start gap-3 rounded-md border border-border/50 p-3 hover:bg-accent/40">
						<RadioGroup.Item value={scope.value} class="mt-0.5" />
						<div class="grid gap-1 leading-none">
							<span class="text-sm font-medium">{scope.label()}</span>
							<span class="text-xs text-muted-foreground">{scope.description()}</span>
						</div>
					</label>
				{/each}
			</div>
		</RadioGroup.Root>
	</div>

	<div class="space-y-2">
		<Label for="op-vault">{m.onepassword_vault()}</Label>
		{#if opVaultsQuery.isPending}
			<div class="flex items-center gap-2 text-sm text-muted-foreground"><Spinner class="size-4" />{m.common_loading()}</div>
		{:else if opVaultsQuery.isError}
			{@render browseError(opVaultsQuery.error)}
		{:else}
			<Select.Root
				type="single"
				value={target[vaultKey].vaultId}
				onValueChange={(value) => {
					const vault = opVaultsQuery.data?.find((candidate) => candidate.id === value);
					const current = target[vaultKey];
					if (current) {
						target[vaultKey] = {
							...current,
							vaultId: value,
							itemId: '',
							name: current.scope === 'vault' ? (vault?.name ?? '') : ''
						};
					}
				}}
			>
				<Select.Trigger id="op-vault" class="w-full">
					<span
						>{opVaultsQuery.data?.find((vault) => vault.id === target[vaultKey]?.vaultId)?.name ??
							m.onepassword_select_vault()}</span
					>
				</Select.Trigger>
				<Select.Content>
					{#each opVaultsQuery.data ?? [] as vault (vault.id)}
						<Select.Item value={vault.id}>{vault.name}</Select.Item>
					{/each}
				</Select.Content>
			</Select.Root>
		{/if}
	</div>

	{#if target[vaultKey].scope === 'item' && target[vaultKey].vaultId}
		<div class="space-y-2">
			<Label for="op-item">{m.onepassword_item()}</Label>
			{#if opItemsQuery.isPending}
				<div class="flex items-center gap-2 text-sm text-muted-foreground"><Spinner class="size-4" />{m.common_loading()}</div>
			{:else if opItemsQuery.isError}
				{@render browseError(opItemsQuery.error)}
			{:else}
				<Select.Root
					type="single"
					value={target[vaultKey].itemId ?? ''}
					onValueChange={(value) => {
						const item = opItemsQuery.data?.find((candidate) => candidate.id === value);
						const current = target[vaultKey];
						if (current) target[vaultKey] = { ...current, itemId: value, name: item?.name ?? '' };
					}}
				>
					<Select.Trigger id="op-item" class="w-full">
						<span>{target[vaultKey].name || m.onepassword_select_item()}</span>
					</Select.Trigger>
					<Select.Content>
						{#each opItemsQuery.data ?? [] as item (item.id)}
							<Select.Item value={item.id}>
								{item.name}
								{#if item.detail}<span class="ml-2 text-xs text-muted-foreground">{item.detail}</span>{/if}
							</Select.Item>
						{/each}
					</Select.Content>
				</Select.Root>
			{/if}
		</div>
	{/if}
{:else if provider === 'http' && target.http}
	<div class="space-y-2">
		<Label for="http-path">{m.http_target_path()}</Label>
		<Input id="http-path" mono placeholder="secrets/immich" bind:value={target.http.path} />
		<p class="text-xs text-muted-foreground">{m.http_target_path_description()}</p>
	</div>
{/if}
