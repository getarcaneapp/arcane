<script lang="ts">
	interface Props {
		message: string;
		padding?: string;
	}

	let { message, padding = 'py-12' }: Props = $props();
</script>

<div class="text-muted-foreground rounded-lg border border-dashed {padding} text-center">
	<div class="text-sm">{message}</div>
</div>
