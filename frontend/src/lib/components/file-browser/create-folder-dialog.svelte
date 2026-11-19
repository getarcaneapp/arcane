<script lang="ts">
	import { toast } from 'svelte-sonner';

	import { Button } from '#lib/components/ui/button/index.js';
	import * as Dialog from '#lib/components/ui/dialog/index.js';
	import { Input } from '#lib/components/ui/input/index.js';
	import { Label } from '#lib/components/ui/label/index.js';
	import * as m from '#lib/paraglide/messages.js';
	import { activityToastOptions, extractActivityId } from '#lib/utils/activity-toast.js';
	import { tryCatch } from '#lib/utils/try-catch.js';

	let {
		open = $bindable(false),
		currentPath,
		onCreate
	}: {
		open: boolean;
		currentPath: string;
		onCreate: (folderName: string) => Promise<unknown>;
	} = $props();

	let folderName = $state('');
	let loading = $state(false);

	async function handleSubmit(e: SubmitEvent) {
		e.preventDefault();
		if (!folderName) return;

		loading = true;
		try {
			const created = await tryCatch(onCreate(folderName));
			if (created.error !== null) {
				toast.error(created.error.message || m.common_create_failed({ resource: folderName }));
				return;
			}
			toast.success(m.common_create_success({ resource: folderName }), activityToastOptions(extractActivityId(created.data)));
			open = false;
			folderName = '';
		} finally {
			loading = false;
		}
	}
</script>

<Dialog.Root {open} onOpenChange={(nextOpen) => (open = nextOpen)}>
	<Dialog.Content class="sm:max-w-106.25">
		<Dialog.Header>
			<Dialog.Title>{m.new_folder()}</Dialog.Title>
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
