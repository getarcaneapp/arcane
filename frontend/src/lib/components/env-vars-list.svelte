<script lang="ts">
	import { KeyValueCard, KeyValueGrid } from '$lib/components/resource-detail';

	interface Props {
		envVars: string[];
		nameOnlyLabel: string;
		valueTitle?: string;
	}

	let { envVars, nameOnlyLabel, valueTitle }: Props = $props();
</script>

<KeyValueGrid>
	{#each envVars as env, index (index)}
		{#if env.includes('=')}
			{@const [key, ...valueParts] = env.split('=')}
			{@const value = valueParts.join('=')}
			<KeyValueCard label={key ?? ''} {valueTitle}>{value}</KeyValueCard>
		{:else}
			<KeyValueCard
				label={nameOnlyLabel}
				labelClass="text-muted-foreground text-xs font-semibold tracking-wide uppercase"
				{valueTitle}
			>
				{env}
			</KeyValueCard>
		{/if}
	{/each}
</KeyValueGrid>
