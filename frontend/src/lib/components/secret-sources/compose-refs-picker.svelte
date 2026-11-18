<script lang="ts">
	import { createQuery } from '@tanstack/svelte-query';
	import type { SvelteSet } from 'svelte/reactivity';

	import * as Alert from '#lib/components/ui/alert/index.js';
	import { Checkbox } from '#lib/components/ui/checkbox/index.js';
	import { Spinner } from '#lib/components/ui/spinner/index.js';
	import { AlertIcon, AlertTriangleIcon, CheckIcon } from '#lib/icons/index.js';
	import { m } from '#lib/paraglide/messages.js';
	import { secretSourceService } from '#lib/services/secret-source-service.js';
	import { extractApiErrorMessage } from '#lib/utils/api.js';

	import { ENV_TARGET, declaredEnvKeys, pairKey } from './compose-refs.js';

	let {
		compose,
		keys,
		selection,
		env = null,
		composeLocked = false
	}: {
		compose: string;
		keys: string[];
		// Pairs of service and key to add; owned by the parent.
		selection: SvelteSet<string>;
		// The project's .env content; null leaves out the .env column.
		env?: string | null;
		// The compose file cannot be edited here (Git manages it), so only .env is offered.
		composeLocked?: boolean;
	} = $props();

	const servicesQuery = createQuery(() => ({
		queryKey: ['secret-sources', 'compose-services', compose],
		queryFn: () => secretSourceService.composeServices(compose),
		enabled: !composeLocked && !!compose.trim(),
		retry: false
	}));
	const services = $derived(composeLocked ? [] : (servicesQuery.data ?? []));
	const editable = $derived(services.filter((service) => service.editable));
	const blocked = $derived(services.filter((service) => !service.editable));
	const showEnv = $derived(env !== null);
	const inEnv = $derived(declaredEnvKeys(env ?? ''));
	// Without a compose file to edit, .env is the only place references can go.
	const ready = $derived(composeLocked ? showEnv : !!compose.trim() && servicesQuery.isSuccess);

	// With a single service, every key goes to it unless the user says otherwise.
	let defaulted = false;
	$effect(() => {
		if (defaulted || composeLocked || !servicesQuery.isSuccess) return;
		defaulted = true;
		const only = editable.length === 1 ? editable[0] : undefined;
		if (!only) return;
		for (const key of keys) {
			if (!only.available.includes(key)) selection.add(pairKey(only.name, key));
		}
	});

	function toggleService(service: string, available: string[]) {
		const open = keys.filter((key) => !available.includes(key));
		const all = open.every((key) => selection.has(pairKey(service, key)));
		for (const key of open) {
			if (all) selection.delete(pairKey(service, key));
			else selection.add(pairKey(service, key));
		}
	}
</script>

{#if composeLocked && !showEnv}
	<p class="text-sm text-muted-foreground">{m.compose_refs_nothing_editable()}</p>
{:else if !composeLocked && !compose.trim()}
	<p class="text-sm text-muted-foreground">{m.compose_refs_no_compose()}</p>
{:else if !composeLocked && servicesQuery.isPending}
	<div class="flex items-center gap-2 text-sm text-muted-foreground"><Spinner class="size-4" />{m.common_loading()}</div>
{:else if !composeLocked && servicesQuery.isError}
	<Alert.Root
		variant="destructive-subtle"
		icon={AlertIcon}
		heading={m.compose_refs_parse_failed()}
		description={extractApiErrorMessage(servicesQuery.error)}
	/>
{:else if keys.length === 0}
	<p class="text-sm text-muted-foreground">{m.compose_refs_no_keys()}</p>
{:else if ready}
	{#if composeLocked}
		<Alert.Root variant="info" icon={AlertIcon} description={m.compose_refs_compose_locked()} />
	{:else}
		<p class="text-xs text-muted-foreground">{m.compose_refs_hint()}</p>
	{/if}
	{#if showEnv}
		<p class="text-xs text-muted-foreground">{m.compose_refs_env_hint({ example: 'KEY=${KEY}' })}</p>
	{/if}
	<div class="overflow-x-auto rounded-lg border border-border/50">
		<table class="w-full text-sm">
			<thead>
				<tr class="border-b border-border/50 text-xs text-muted-foreground">
					<th class="px-3 py-2 text-left font-medium">{m.compose_refs_key()}</th>
					{#each editable as service (service.name)}
						<th class="px-3 py-2 text-center font-medium">
							<button
								type="button"
								class="font-mono hover:text-foreground"
								title={m.compose_refs_toggle_service()}
								onclick={() => toggleService(service.name, service.available)}
							>
								{service.name}
							</button>
						</th>
					{/each}
					{#if showEnv}
						<th class="border-l border-border/50 px-3 py-2 text-center font-medium">
							<button
								type="button"
								class="font-mono hover:text-foreground"
								title={m.compose_refs_toggle_env()}
								onclick={() => toggleService(ENV_TARGET, [...inEnv])}
							>
								.env
							</button>
						</th>
					{/if}
				</tr>
			</thead>
			<tbody>
				{#each keys as key (key)}
					<tr class="border-b border-border/50 last:border-0">
						<td class="px-3 py-2 font-mono">{key}</td>
						{#each editable as service (service.name)}
							<td class="px-3 py-2 text-center">
								{#if service.available.includes(key)}
									<span class="inline-flex" title={m.compose_refs_already()}>
										<CheckIcon class="size-4 text-success" />
									</span>
								{:else}
									<Checkbox
										aria-label={`${key} → ${service.name}`}
										checked={selection.has(pairKey(service.name, key))}
										onCheckedChange={(checked) =>
											checked === true ? selection.add(pairKey(service.name, key)) : selection.delete(pairKey(service.name, key))}
									/>
								{/if}
							</td>
						{/each}
						{#if showEnv}
							<td class="border-l border-border/50 px-3 py-2 text-center">
								{#if inEnv.has(key)}
									<span class="inline-flex" title={m.compose_refs_env_already()}>
										<CheckIcon class="size-4 text-success" />
									</span>
								{:else}
									<Checkbox
										aria-label={`${key} → .env`}
										checked={selection.has(pairKey(ENV_TARGET, key))}
										onCheckedChange={(checked) =>
											checked === true ? selection.add(pairKey(ENV_TARGET, key)) : selection.delete(pairKey(ENV_TARGET, key))}
									/>
								{/if}
							</td>
						{/if}
					</tr>
				{/each}
			</tbody>
		</table>
	</div>
	{#if blocked.length > 0}
		<Alert.Root
			variant="warning-subtle"
			icon={AlertTriangleIcon}
			heading={m.compose_refs_blocked()}
			description={blocked.map((service) => `${service.name}: ${service.reason ?? ''}`).join(' · ')}
		/>
	{/if}
{/if}
