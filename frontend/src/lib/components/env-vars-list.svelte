<script lang="ts">
	import { KeyValueCard, KeyValueGrid } from '#lib/components/resource-detail/index.js';
	import { withOccurrenceKeys } from '#lib/utils/formatting.js';

	interface Props {
		envVars: string[];
		nameOnlyLabel: string;
		valueTitle?: string;
	}

	let { envVars, nameOnlyLabel, valueTitle }: Props = $props();
</script>

<KeyValueGrid>
	{#each withOccurrenceKeys(envVars) as { value: env, key: entryKey } (entryKey)}
		{#if env.includes('=')}
			{@const [key, ...valueParts] = env.split('=')}
			{@const value = valueParts.join('=')}
			<KeyValueCard label={key ?? ''} {valueTitle}>{value}</KeyValueCard>
		{:else}
			<KeyValueCard label={nameOnlyLabel} {valueTitle}>
				{env}
			</KeyValueCard>
		{/if}
	{/each}
</KeyValueGrid>
