<script lang="ts">
	import type { Component } from 'svelte';
	// Brand marks from the selfh.st icon set (CC BY 4.0), the same set Arcane
	// offers as an icon catalog. They are bundled, so no request leaves Arcane.
	import OnePasswordIcon from 'virtual:icons/selfhst/1password';
	import OnePasswordLightIcon from 'virtual:icons/selfhst/1password-light';
	import BitwardenIcon from 'virtual:icons/selfhst/bitwarden';
	import DopplerIcon from 'virtual:icons/selfhst/doppler';
	import VaultIcon from 'virtual:icons/selfhst/hashicorp-vault';
	import InfisicalIcon from 'virtual:icons/selfhst/infisical';
	import InfisicalLightIcon from 'virtual:icons/selfhst/infisical-light';
	import ProtonPassIcon from 'virtual:icons/selfhst/proton-pass';

	import { ConnectionIcon } from '#lib/icons/index.js';
	import type { SecretProvider } from '#lib/types/secret-source.js';
	import { cn } from '#lib/utils.js';

	let { provider, class: className }: { provider: SecretProvider | undefined; class?: string } = $props();

	// `dark` replaces marks that are too dark to see on the dark theme.
	const brandIcons: Partial<Record<SecretProvider, { light: Component; dark?: Component }>> = {
		bitwarden: { light: BitwardenIcon },
		doppler: { light: DopplerIcon },
		infisical: { light: InfisicalIcon, dark: InfisicalLightIcon },
		onepassword: { light: OnePasswordIcon, dark: OnePasswordLightIcon },
		protonpass: { light: ProtonPassIcon },
		vault: { light: VaultIcon }
	};

	const brand = $derived(provider ? brandIcons[provider] : undefined);
</script>

<span
	class={cn(
		'inline-flex size-8 shrink-0 items-center justify-center rounded-lg bg-card p-1.5 text-muted-foreground ring-1 ring-border',
		className
	)}
	aria-hidden="true"
>
	{#if brand?.dark}
		<brand.light class="size-full dark:hidden" />
		<brand.dark class="hidden size-full dark:block" />
	{:else if brand}
		<brand.light class="size-full" />
	{:else}
		<ConnectionIcon class="size-full" />
	{/if}
</span>
