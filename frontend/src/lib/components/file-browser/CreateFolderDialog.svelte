<script lang="ts">
	import * as Dialog from '$lib/components/ui/dialog';
	import { Button } from '$lib/components/ui/button';
	import { Input } from '$lib/components/ui/input';
	import { Label } from '$lib/components/ui/label';
	import { toast } from 'svelte-sonner';
	import * as m from '$lib/paraglide/messages.js';

	let {
		open = $bindable(false),
		currentPath,
		onCreate
	}: {
		open: boolean;
		currentPath: string;
		onCreate: (folderName: string) => Promise<void>;
	} = $props();

	let folderName = $state('');
	let loading = $state(false);

	async function handleSubmit(e: SubmitEvent) {
		e.preventDefault();
		if (!folderName) return;

		loading = true;
		try {
			await onCreate(folderName);
			toast.success(m.common_create_success({ resource: folderName }));
			open = false;
			folderName = '';
		} catch (e: any) {
			toast.error(e.message || m.common_create_failed({ resource: folderName }));
		} finally {
			loading = false;
		}
	}
</script>

<Dialog.Root bind:open>
	<Dialog.Content class="sm:max-w-[425px]">
		<Dialog.Header>
			<Dialog.Title>{m.volumes_browser_new_folder()}</Dialog.Title>
			<Dialog.Description>
				Enter a name for the new folder in {currentPath}
			</Dialog.Description>
		</Dialog.Header>
		<form onsubmit={handleSubmit} class="grid gap-4 py-4">
			<div class="grid grid-cols-4 items-center gap-4">
				<Label for="name" class="text-right">{m.common_name()}</Label>
				<Input id="name" bind:value={folderName} class="col-span-3" autofocus />
			</div>
			<Dialog.Footer>
				<Button type="button" variant="outline" onclick={() => (open = false)}>{m.common_cancel()}</Button>
				<Button type="submit" disabled={loading || !folderName}>
					{loading ? m.common_action_creating() : m.common_create()}
				</Button>
			</Dialog.Footer>
		</form>
	</Dialog.Content>
</Dialog.Root>
