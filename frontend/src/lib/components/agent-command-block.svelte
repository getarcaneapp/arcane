<script lang="ts">
	import { CopyButton } from '$lib/components/ui/copy-button';

	interface Props {
		dockerRunLabel: string;
		dockerRun: string;
		dockerComposeLabel: string;
		dockerCompose: string;
		preClass?: string;
	}

	let { dockerRunLabel, dockerRun, dockerComposeLabel, dockerCompose, preClass = '' }: Props = $props();
</script>

<div class="space-y-2">
	<div class="text-sm font-medium">{dockerRunLabel}</div>
	<div class="relative">
		<pre class="bg-muted overflow-x-auto rounded-md p-3 text-xs {preClass}"><code>{dockerRun}</code></pre>
		<div class="absolute top-2 right-2">
			<CopyButton text={dockerRun} size="icon" class="size-7" />
		</div>
	</div>
</div>

<div class="space-y-2">
	<div class="text-sm font-medium">{dockerComposeLabel}</div>
	<div class="relative">
		<pre class="bg-muted overflow-x-auto rounded-md p-3 text-xs {preClass}"><code>{dockerCompose}</code></pre>
		<div class="absolute top-2 right-2">
			<CopyButton text={dockerCompose} size="icon" class="size-7" />
		</div>
	</div>
</div>
