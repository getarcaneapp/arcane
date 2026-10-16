<script lang="ts">
	import { m } from '#lib/paraglide/messages.js';
	import { formatElapsedTime, nowInstantString, type InstantInput } from '#lib/utils/formatting.js';

	let { since, class: className }: { since: InstantInput; class?: string } = $props();

	// Owns the one-second tick so only this text updates while a stage is running.
	let clock = $state(nowInstantString());

	$effect(() => {
		const ticker = setInterval(() => (clock = nowInstantString()), 1000);
		return () => clearInterval(ticker);
	});
</script>

<span class={className}>
	{m.environments_update_all_elapsed({
		duration: formatElapsedTime(since, { base: clock })
	})}
</span>
