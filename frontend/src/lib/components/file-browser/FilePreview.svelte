<script lang="ts">
	import type { FileEntry } from '$lib/types/file-browser.type';
	import { onMount } from 'svelte';
	import * as Sheet from '$lib/components/ui/sheet';
	import { LoadingSpinnerIcon } from '$lib/icons';

	let { file, fetchContent, onClose }: { file: FileEntry; fetchContent: (path: string) => Promise<string>; onClose: () => void } =
		$props();

	let content = $state<string | null>(null);
	let loading = $state(true);
	let error = $state<string | null>(null);

	function b64DecodeUnicode(str: string) {
		try {
			// Try to decode UTF-8 safely
			return decodeURIComponent(
				atob(str)
					.split('')
					.map(function (c) {
						return '%' + ('00' + c.charCodeAt(0).toString(16)).slice(-2);
					})
					.join('')
			);
		} catch (e) {
			// Fallback to plain atob if it's not proper UTF-8
			return atob(str);
		}
	}

	onMount(async () => {
		try {
			const res = await fetchContent(file.path);
			content = b64DecodeUnicode(res);
		} catch (e: any) {
			error = e.message || 'Failed to load preview';
		} finally {
			loading = false;
		}
	});
</script>

<Sheet.Root open={!!file} onOpenChange={(open) => !open && onClose()}>
	<Sheet.Content class="flex h-full flex-col sm:max-w-2xl">
		<Sheet.Header>
			<Sheet.Title class="truncate">{file.name}</Sheet.Title>
			<Sheet.Description class="break-all">{file.path}</Sheet.Description>
		</Sheet.Header>

		<div class="mt-6 min-h-0 flex-grow overflow-y-auto">
			{#if loading}
				<div class="flex h-full items-center justify-center p-12">
					<LoadingSpinnerIcon class="text-muted-foreground h-8 w-8" />
				</div>
			{:else if error}
				<div class="border-destructive/20 bg-destructive/10 text-destructive rounded border p-4">
					{error}
				</div>
			{:else}
				<pre class="bg-muted w-full rounded p-4 font-mono text-xs break-all whitespace-pre-wrap">{content}</pre>
			{/if}
		</div>
	</Sheet.Content>
</Sheet.Root>
