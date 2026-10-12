<script lang="ts">
	import { onMount } from 'svelte';
	import { SvelteSet } from 'svelte/reactivity';
	import { slide } from 'svelte/transition';

	import { ArcaneButton } from '#lib/components/arcane-button/index.js';
	import SearchInput from '#lib/components/form/search-input.svelte';
	import * as Alert from '#lib/components/ui/alert/index.js';
	import * as Table from '#lib/components/ui/table/index.js';
	import { ArrowRightIcon } from '#lib/icons/index.js';
	import { m } from '#lib/paraglide/messages.js';
	import { diagnosticsService } from '#lib/services/diagnostics-service.js';
	import { LOCAL_DOCKER_ENVIRONMENT_ID } from '#lib/stores/environment.store.svelte.js';
	import type { GoroutineGroup, GoroutineProfile } from '#lib/types/diagnostics.js';
	import { cn } from '#lib/utils.js';
	import { extractApiErrorMessage } from '#lib/utils/api.js';
	import { tryCatch } from '#lib/utils/try-catch.js';

	let profile = $state.raw<GoroutineProfile | null>(null);
	let loading = $state(false);
	let error = $state<string | null>(null);
	let query = $state('');
	const expanded = new SvelteSet<string>();

	// The backend groups by identical stack, so the frames identify a group across refreshes.
	const groups = $derived(
		(profile?.groups ?? []).map((group) => ({
			key: group.frames.map((f) => `${f.function}@${f.file}:${f.line}`).join('|'),
			group
		}))
	);

	const filtered = $derived.by(() => {
		const q = query.trim().toLowerCase();
		if (!q) return groups;
		return groups.filter(({ group }) => matches(group, q));
	});

	function matches(group: GoroutineGroup, q: string): boolean {
		return (
			group.label.toLowerCase().includes(q) ||
			group.frames.some((f) => f.function.toLowerCase().includes(q) || f.file.toLowerCase().includes(q))
		);
	}

	function toggle(key: string) {
		if (expanded.has(key)) expanded.delete(key);
		else expanded.add(key);
	}

	async function load() {
		loading = true;
		const result = await tryCatch(diagnosticsService.getGoroutinesForEnvironment(LOCAL_DOCKER_ENVIRONMENT_ID));
		loading = false;
		if (result.error !== null) {
			error = extractApiErrorMessage(result.error);
			return;
		}
		profile = result.data;
		error = null;
	}

	onMount(load);
</script>

<div class="space-y-4">
	<p class="text-sm text-muted-foreground">{m.diagnostics_goroutines_hint()}</p>

	<div class="flex flex-wrap items-center gap-3">
		{#if profile}
			<span class="text-sm font-medium tabular-nums">
				{m.diagnostics_goroutines_summary({ total: profile.total, groups: profile.groups.length })}
			</span>
		{/if}
		<div class="min-w-48 flex-1">
			<SearchInput
				bind:value={query}
				onClear={() => (query = '')}
				placeholder={m.diagnostics_goroutines_filter_placeholder()}
				aria-label={m.common_search()}
			/>
		</div>
		<ArcaneButton action="refresh" size="sm" {loading} onclick={load} />
	</div>

	{#if error}
		<Alert.Root variant="destructive">
			<Alert.Title>{m.common_unavailable()}</Alert.Title>
			<Alert.Description>{error}</Alert.Description>
		</Alert.Root>
	{:else if !profile}
		<p class="py-8 text-sm text-muted-foreground">{m.common_loading()}</p>
	{:else if filtered.length}
		<div class="overflow-x-auto">
			<Table.Root>
				<Table.Header>
					<Table.Row variant="static">
						<Table.Head variant="expander"></Table.Head>
						<Table.Head>{m.diagnostics_goroutines_count()}</Table.Head>
						<Table.Head>{m.diagnostics_goroutines_entry_point()}</Table.Head>
					</Table.Row>
				</Table.Header>
				<Table.Body>
					{#each filtered as { key, group } (key)}
						{@const open = expanded.has(key)}
						<Table.Row data-expanded={open ? true : undefined} onclick={() => toggle(key)}>
							<Table.Cell variant="expander">
								<ArrowRightIcon
									class={cn('size-4 text-muted-foreground transition-transform duration-200', open && 'rotate-90')}
									aria-hidden="true"
								/>
							</Table.Cell>
							<Table.Cell><span class="font-semibold tabular-nums">{group.count}</span></Table.Cell>
							<Table.Cell><span class="font-mono text-xs break-all">{group.label}</span></Table.Cell>
						</Table.Row>
						{#if open}
							<Table.Row variant="detail">
								<Table.Cell colspan={3} variant="flush">
									<ol transition:slide={{ duration: 200 }} class="space-y-1.5 px-6 py-4 font-mono text-2xs">
										{#each group.frames as frame}
											<li>
												<div class="break-all">{frame.function}</div>
												<div class="break-all text-muted-foreground">{frame.file}:{frame.line}</div>
											</li>
										{/each}
									</ol>
								</Table.Cell>
							</Table.Row>
						{/if}
					{/each}
				</Table.Body>
			</Table.Root>
		</div>
	{:else}
		<p class="py-8 text-sm text-muted-foreground">{m.common_no_results_found()}</p>
	{/if}
</div>
