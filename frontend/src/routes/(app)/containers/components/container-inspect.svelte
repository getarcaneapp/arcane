<script lang="ts">
	import { ArcaneButton } from '#lib/components/arcane-button/index.js';
	import { InspectJsonCard } from '#lib/components/resource-detail/index.js';
	import { EyeOffIcon, EyeOnIcon } from '#lib/icons/index.js';
	import { m } from '#lib/paraglide/messages.js';
	import type { ContainerDetails } from '#lib/types/container.js';
	import { isSensitiveEnvKey } from '#lib/utils/formatting.js';

	interface Props {
		container: ContainerDetails;
	}

	let { container }: Props = $props();

	let showSecrets = $state(false);

	const maskedEnv = $derived(
		(container.config?.env ?? []).map((env) => {
			const separator = env.indexOf('=');
			if (separator === -1) return env;
			const key = env.slice(0, separator);
			return isSensitiveEnvKey(key, env.slice(separator + 1)) ? `${key}=********` : env;
		})
	);
	const hasSensitive = $derived(maskedEnv.some((env, index) => env !== container.config?.env?.[index]));
	const json = $derived(
		JSON.stringify(
			hasSensitive && !showSecrets ? { ...container, config: { ...container.config, env: maskedEnv } } : container,
			null,
			2
		)
	);
</script>

<InspectJsonCard description={m.containers_inspect_description()} {json}>
	{#snippet actions()}
		{#if hasSensitive}
			<ArcaneButton
				action="base"
				icon={showSecrets ? EyeOffIcon : EyeOnIcon}
				customLabel={showSecrets ? m.common_hide() : m.common_show()}
				onclick={() => (showSecrets = !showSecrets)}
			/>
		{/if}
	{/snippet}
</InspectJsonCard>
