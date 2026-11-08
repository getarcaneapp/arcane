<script lang="ts">
	import { SvelteSet } from 'svelte/reactivity';

	import { ArcaneButton } from '#lib/components/arcane-button/index.js';
	import { KeyValueCard, KeyValueGrid } from '#lib/components/resource-detail/index.js';
	import { EyeOffIcon, EyeOnIcon } from '#lib/icons/index.js';
	import { m } from '#lib/paraglide/messages.js';
	import { isSensitiveEnvKey, withOccurrenceKeys } from '#lib/utils/formatting.js';

	interface Props {
		envVars: string[];
		nameOnlyLabel: string;
		valueTitle?: string;
	}

	let { envVars, nameOnlyLabel, valueTitle }: Props = $props();

	const revealed = new SvelteSet<string>();
	let revealAll = $state(false);

	let entries = $derived(
		withOccurrenceKeys(envVars).map(({ value: env, key: entryKey }) => {
			const separator = env.indexOf('=');
			const key = separator === -1 ? null : env.slice(0, separator);
			const value = separator === -1 ? env : env.slice(separator + 1);
			return {
				entryKey,
				key,
				value,
				sensitive: key !== null && isSensitiveEnvKey(key, value)
			};
		})
	);
	let hasSensitive = $derived(entries.some((entry) => entry.sensitive));

	function toggleAll() {
		revealAll = !revealAll;
		revealed.clear();
	}

	function toggleRow(entryKey: string) {
		if (revealAll) {
			revealAll = false;
			for (const entry of entries) {
				if (entry.sensitive && entry.entryKey !== entryKey) revealed.add(entry.entryKey);
			}
			return;
		}
		if (revealed.has(entryKey)) {
			revealed.delete(entryKey);
		} else {
			revealed.add(entryKey);
		}
	}
</script>

{#if hasSensitive}
	<div class="flex justify-end">
		<ArcaneButton
			action="base"
			icon={revealAll ? EyeOffIcon : EyeOnIcon}
			customLabel={revealAll ? m.common_hide() : m.common_show()}
			onclick={toggleAll}
		/>
	</div>
{/if}

<KeyValueGrid>
	{#each entries as { entryKey, key, value, sensitive } (entryKey)}
		{#if key === null}
			<KeyValueCard label={nameOnlyLabel} {valueTitle}>
				{value}
			</KeyValueCard>
		{:else if sensitive}
			{@const shown = revealAll || revealed.has(entryKey)}
			<KeyValueCard label={key} {valueTitle}>
				<span class="flex items-start gap-2">
					{#if shown}
						<span class="min-w-0 flex-1">{value}</span>
					{:else}
						<span class="min-w-0 flex-1 blur-sm select-none" aria-hidden="true" onclick={() => revealed.add(entryKey)}>
							{value}
						</span>
					{/if}
					<ArcaneButton
						action="base"
						tone="ghost"
						size="icon"
						class="size-6 shrink-0"
						showLabel={false}
						icon={shown ? EyeOffIcon : EyeOnIcon}
						customLabel={shown ? m.hide_name({ name: key }) : m.reveal_value()}
						onclick={() => toggleRow(entryKey)}
					/>
				</span>
			</KeyValueCard>
		{:else}
			<KeyValueCard label={key} {valueTitle}>{value}</KeyValueCard>
		{/if}
	{/each}
</KeyValueGrid>
