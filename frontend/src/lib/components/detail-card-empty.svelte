<script lang="ts">
	interface Props {
		message: string;
		padding?: string;
	}

	let { message, padding = 'py-12' }: Props = $props();
</script>

<div class="rounded-lg border border-dashed text-muted-foreground {padding} text-center">
	<div class="text-sm">{message}</div>
</div>
