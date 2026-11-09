<script lang="ts">
	import { Input } from '#lib/components/ui/input/index.js';
	import { Label } from '#lib/components/ui/label/index.js';
	import { m } from '#lib/paraglide/messages.js';

	let { value = $bindable('') }: { value?: string } = $props();
</script>

<div class="space-y-2">
	<Label for="remote-context-url">{m.git_repository_url()}</Label>
	<Input
		id="remote-context-url"
		bind:value
		placeholder={m.build_remote_git_context_placeholder()}
		spellcheck="false"
		autocomplete="off"
	/>
	<p class="text-xs text-muted-foreground">{m.build_remote_git_context_description()}</p>
</div>
