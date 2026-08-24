<script lang="ts">
	import { ArcaneButton } from '#lib/components/arcane-button';
	import { Input } from '#lib/components/ui/input';
	import { ScrollArea } from '#lib/components/ui/scroll-area';
	import * as Checkbox from '#lib/components/ui/checkbox';
	import { LoadingSpinnerIcon } from '#lib/icons';
	import * as m from '#lib/paraglide/messages.js';

	let {
		files,
		loading = false,
		selectedPaths = $bindable(),
		search = $bindable()
	}: {
		files: string[];
		loading?: boolean;
		selectedPaths: string[];
		search: string;
	} = $props();

	const filteredFiles = $derived.by(() => {
		const query = search.trim().toLowerCase();
		if (!query) return files;
		return files.filter((file) => file.toLowerCase().includes(query));
	});

	function togglePath(path: string, checked: boolean) {
		if (checked) {
			if (!selectedPaths.includes(path)) selectedPaths = [...selectedPaths, path];
			return;
		}
		selectedPaths = selectedPaths.filter((selected) => selected !== path);
	}

	function selectAllVisible() {
		const next = new Set(selectedPaths);
		for (const file of filteredFiles) next.add(file);
		selectedPaths = Array.from(next);
	}
</script>

<div class="space-y-3">
	<div class="flex items-center justify-between gap-2">
		<Input class="h-9" placeholder={m.volume_search_files()} bind:value={search} />
		<div class="flex items-center gap-2">
			<ArcaneButton action="base" tone="ghost" size="sm" onclick={selectAllVisible} customLabel={m.common_select_all()} />
			<ArcaneButton action="base" tone="ghost" size="sm" onclick={() => (selectedPaths = [])} customLabel={m.common_clear()} />
		</div>
	</div>

	<ScrollArea class="h-64 rounded-md border">
		{#if loading}
			<div class="flex items-center justify-center py-8">
				<LoadingSpinnerIcon class="size-5 text-muted-foreground" />
			</div>
		{:else if filteredFiles.length === 0}
			<div class="flex items-center justify-center py-8 text-sm text-muted-foreground">{m.volume_backup_no_files()}</div>
		{:else}
			<div class="divide-y divide-border/40">
				{#each filteredFiles as filePath (filePath)}
					<div class="flex items-center gap-3 px-3 py-2">
						<Checkbox.Root
							checked={selectedPaths.includes(filePath)}
							onCheckedChange={(value) => togglePath(filePath, !!value)}
						/>
						<code class="font-mono text-xs break-all">{filePath}</code>
					</div>
				{/each}
			</div>
		{/if}
	</ScrollArea>
</div>
