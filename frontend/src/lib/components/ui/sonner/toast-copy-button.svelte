<script lang="ts">
	import { CopyButton } from '#lib/components/ui/copy-button/index.js';

	let text = $state('');
</script>

<div
	role="presentation"
	class="shrink-0"
	onclickcapture={(event) => {
		text = event.currentTarget.closest('[data-sonner-toast]')?.querySelector<HTMLElement>('[data-content]')?.innerText ?? '';
	}}
>
	<CopyButton {text} tabindex={0} />
</div>
