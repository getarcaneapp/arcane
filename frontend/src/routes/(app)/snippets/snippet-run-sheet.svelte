<script lang="ts">
	import * as ResponsiveDialog from '#lib/components/ui/responsive-dialog/index.js';
	import { Button } from '#lib/components/ui/button';
	import { Badge } from '#lib/components/ui/badge';
	import TextInputWithLabel from '#lib/components/form/text-input-with-label.svelte';
	import SelectWithLabel from '#lib/components/form/select-with-label.svelte';
	import SwitchWithLabel from '#lib/components/form/labeled-switch.svelte';
	import { snippetService } from '#lib/services/snippet-service';
	import type { Snippet, SnippetRun, SnippetParameterType } from '#lib/types/snippet';
	import { toast } from 'svelte-sonner';
	import { m } from '#lib/paraglide/messages';
	import { formatDateTime } from '#lib/utils/formatting';
	import { MonitorIcon, BoxIcon } from '#lib/icons';

	let {
		open = $bindable(false),
		snippet,
		environmentId
	}: {
		open: boolean;
		snippet: Snippet | null;
		environmentId: string;
	} = $props();

	interface RunField {
		name: string;
		type: SnippetParameterType;
		label: string;
		required: boolean;
		options: string[];
		value: string;
		checked: boolean;
	}

	let runFields = $state<RunField[]>([]);
	let isRunning = $state(false);
	let activeRun = $state<SnippetRun | null>(null);
	let recentRuns = $state<SnippetRun[]>([]);
	let isLoadingRuns = $state(false);

	async function loadRecentRuns() {
		if (!snippet) return;
		isLoadingRuns = true;
		try {
			const page = await snippetService.getSnippetRuns(environmentId, snippet.id, {
				pagination: { page: 1, limit: 10 },
				sort: { column: 'startedAt', direction: 'desc' }
			});
			recentRuns = page.data;
		} catch {
			recentRuns = [];
		} finally {
			isLoadingRuns = false;
		}
	}

	$effect(() => {
		if (open && snippet) {
			runFields = (snippet.parameters ?? []).map((def) => ({
				name: def.name,
				type: def.type,
				label: def.label || def.name,
				required: def.required ?? false,
				options: def.options ?? [],
				value: def.default ?? '',
				checked: def.default === 'true'
			}));
			activeRun = null;
			void loadRecentRuns();
		}
	});

	function resolveParameters(): Record<string, string> {
		const resolved: Record<string, string> = {};
		for (const field of runFields) {
			resolved[field.name] = field.type === 'boolean' ? (field.checked ? 'true' : 'false') : field.value;
		}
		return resolved;
	}

	async function handleRun() {
		if (!snippet) return;
		isRunning = true;
		try {
			const run = await snippetService.runSnippet(environmentId, snippet.id, { parameters: resolveParameters() });
			activeRun = run;
			recentRuns = [run, ...recentRuns].slice(0, 10);
			if (run.status === 'success') {
				toast.success(m.snippets_run_success());
			} else {
				toast.warning(m.snippets_run_finished_with_status({ status: run.status }));
			}
		} catch (error) {
			toast.error(error instanceof Error ? error.message : m.snippets_run_failed());
		} finally {
			isRunning = false;
		}
	}

	function statusVariant(status: string): 'green' | 'red' | 'amber' | 'gray' {
		if (status === 'success') return 'green';
		if (status === 'failed' || status === 'timeout') return 'red';
		return 'amber';
	}
</script>

<ResponsiveDialog.Root
	bind:open
	variant="sheet"
	title={m.snippets_run_title({ name: snippet?.name ?? '' })}
	description={m.snippets_run_description()}
	contentClass="sm:max-w-[600px]"
>
	{#snippet children()}
		<div class="grid gap-4 py-4">
			<Badge variant="outline" size="sm" class="w-fit gap-1">
				{#if snippet?.target === 'container'}
					<BoxIcon class="size-3" />
					{m.snippets_target_container()}
				{:else}
					<MonitorIcon class="size-3" />
					{m.snippets_target_host()}
				{/if}
			</Badge>

			{#if runFields.length > 0}
				<div class="grid gap-3">
					{#each runFields as field (field.name)}
						{#if field.type === 'boolean'}
							<SwitchWithLabel id={`run-param-${field.name}`} label={field.label} bind:checked={field.checked} />
						{:else if field.type === 'select'}
							<SelectWithLabel
								id={`run-param-${field.name}`}
								label={field.label}
								bind:value={field.value}
								options={field.options.map((o) => ({ label: o, value: o }))}
							/>
						{:else}
							<TextInputWithLabel
								id={`run-param-${field.name}`}
								label={field.label}
								type={field.type === 'number' ? 'number' : 'text'}
								required={field.required}
								bind:value={field.value}
							/>
						{/if}
					{/each}
				</div>
			{/if}

			<Button onclick={handleRun} disabled={isRunning}>
				{isRunning ? m.snippets_running() : m.snippets_run()}
			</Button>

			{#if activeRun}
				<div class="space-y-2">
					<div class="flex items-center gap-2">
						<Badge variant={statusVariant(activeRun.status)} size="sm">{activeRun.status}</Badge>
						{#if activeRun.exitCode !== undefined}
							<span class="text-xs text-muted-foreground">{m.snippets_exit_code({ code: activeRun.exitCode })}</span>
						{/if}
						<span class="text-xs text-muted-foreground">{activeRun.durationMs}ms</span>
					</div>
					<pre
						class="max-h-64 overflow-auto rounded-md border border-border/50 bg-muted/30 p-3 font-mono text-xs whitespace-pre-wrap">{activeRun.output ||
							activeRun.error ||
							m.snippets_no_output()}</pre>
				</div>
			{/if}

			{#if recentRuns.length > 0}
				<details class="rounded-md border border-border/50">
					<summary class="cursor-pointer px-3 py-2 text-sm font-medium">{m.snippets_recent_runs()}</summary>
					<ul class="max-h-48 divide-y divide-border/50 overflow-y-auto">
						{#each recentRuns as run (run.id)}
							<li>
								<button
									type="button"
									class="flex w-full items-center justify-between gap-3 px-3 py-2 text-left text-sm hover:bg-accent/40"
									onclick={() => (activeRun = run)}
								>
									<span class="flex items-center gap-2">
										<Badge variant={statusVariant(run.status)} size="sm">{run.status}</Badge>
										<span class="text-xs text-muted-foreground">{run.triggerSource}</span>
									</span>
									<span class="text-xs text-muted-foreground">{formatDateTime(run.startedAt)}</span>
								</button>
							</li>
						{/each}
					</ul>
				</details>
			{:else if !isLoadingRuns}
				<p class="text-xs text-muted-foreground">{m.snippets_no_runs_yet()}</p>
			{/if}
		</div>
	{/snippet}

	{#snippet footer()}
		<Button variant="outline" onclick={() => (open = false)}>{m.common_close()}</Button>
	{/snippet}
</ResponsiveDialog.Root>
