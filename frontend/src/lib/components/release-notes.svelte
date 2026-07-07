<script lang="ts">
	import { marked } from 'marked';
	import DOMPurify from 'isomorphic-dompurify';

	let { markdown }: { markdown: string } = $props();

	const html = $derived.by(() => {
		if (!markdown) return '';
		try {
			const parsed = marked.parse(markdown, { async: false }) as string;
			return DOMPurify.sanitize(parsed, {
				ADD_ATTR: ['target', 'rel']
			});
		} catch {
			return '';
		}
	});
</script>

{#if html}
	<div class="release-notes text-sm leading-relaxed">
		<!-- eslint-disable-next-line svelte/no-at-html-tags -->
		{@html html}
	</div>
{/if}

<style>
	.release-notes :global(h1),
	.release-notes :global(h2),
	.release-notes :global(h3) {
		font-weight: 600;
		margin-top: 1rem;
		margin-bottom: 0.5rem;
		color: var(--foreground);
	}
	.release-notes :global(h1) {
		font-size: 1rem;
	}
	.release-notes :global(h2) {
		font-size: 0.95rem;
	}
	.release-notes :global(h3) {
		font-size: 0.9rem;
	}
	.release-notes :global(h1:first-child),
	.release-notes :global(h2:first-child),
	.release-notes :global(h3:first-child) {
		margin-top: 0;
	}
	.release-notes :global(p) {
		margin: 0.5rem 0;
	}
	.release-notes :global(ul),
	.release-notes :global(ol) {
		margin: 0.5rem 0;
		padding-left: 1.25rem;
	}
	.release-notes :global(ul) {
		list-style: disc;
	}
	.release-notes :global(ol) {
		list-style: decimal;
	}
	.release-notes :global(li) {
		margin: 0.25rem 0;
	}
	.release-notes :global(li > p) {
		margin: 0;
	}
	.release-notes :global(a) {
		color: var(--primary);
		text-decoration: underline;
		text-underline-offset: 2px;
	}
	.release-notes :global(a:hover) {
		opacity: 0.85;
	}
	.release-notes :global(code) {
		background: var(--muted);
		color: var(--foreground);
		padding: 0.1em 0.35em;
		border-radius: 0.25rem;
		font-size: 0.85em;
		font-family: ui-monospace, SFMono-Regular, monospace;
	}
	.release-notes :global(pre) {
		background: var(--muted);
		padding: 0.75rem;
		border-radius: 0.5rem;
		overflow-x: auto;
		margin: 0.5rem 0;
	}
	.release-notes :global(pre code) {
		background: transparent;
		padding: 0;
	}
	.release-notes :global(blockquote) {
		border-left: 2px solid var(--border);
		padding-left: 0.75rem;
		color: var(--muted-foreground);
		margin: 0.5rem 0;
	}
	.release-notes :global(hr) {
		border: 0;
		border-top: 1px solid var(--border);
		margin: 1rem 0;
	}
	.release-notes :global(strong) {
		font-weight: 600;
	}
</style>
