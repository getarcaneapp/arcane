<script lang="ts">
	import { m } from '#lib/paraglide/messages.js';

	let { markdown }: { markdown: string } = $props();

	// marked and DOMPurify load on first render so they stay out of the app shell bundle.
	const renderMarkdown = Promise.all([import('marked'), import('marked-alert'), import('isomorphic-dompurify')]).then(
		([{ Marked }, { default: markedAlert }, { default: DOMPurify }]) => {
			const marked = new Marked(markedAlert());
			return (source: string): string => {
				if (!source) return '';
				try {
					const parsed = marked.parse(source, { async: false }) as string;
					return DOMPurify.sanitize(parsed, {
						ADD_ATTR: ['target', 'rel']
					});
				} catch {
					return '';
				}
			};
		}
	);
</script>

{#await renderMarkdown then render}
	{@const html = render(markdown)}
	{#if html}
		<div class="release-notes text-sm leading-relaxed">
			<!-- eslint-disable-next-line svelte/no-at-html-tags -->
			{@html html}
		</div>
	{/if}
{:catch}
	<p class="text-sm text-destructive">{m.release_notes_load_failed()}</p>
{/await}

<style>
	.release-notes :global(> :first-child) {
		margin-top: 0;
	}
	.release-notes :global(> :last-child) {
		margin-bottom: 0;
	}
	.release-notes :global(h1),
	.release-notes :global(h2),
	.release-notes :global(h3),
	.release-notes :global(h4) {
		font-weight: 600;
		line-height: 1.25;
		margin-top: 1.25rem;
		margin-bottom: 0.75rem;
		color: var(--foreground);
	}
	.release-notes :global(h1),
	.release-notes :global(h2) {
		padding-bottom: 0.3em;
		border-bottom: 1px solid var(--border);
	}
	.release-notes :global(h1) {
		font-size: 1.2rem;
	}
	.release-notes :global(h2) {
		font-size: 1.05rem;
	}
	.release-notes :global(h3) {
		font-size: 0.95rem;
	}
	.release-notes :global(h4) {
		font-size: 0.875rem;
	}
	.release-notes :global(p) {
		margin: 0 0 0.75rem;
	}
	.release-notes :global(ul),
	.release-notes :global(ol) {
		margin: 0 0 0.75rem;
		padding-left: 1.5rem;
	}
	.release-notes :global(ul) {
		list-style: disc;
	}
	.release-notes :global(ol) {
		list-style: decimal;
	}
	.release-notes :global(ul ul) {
		list-style: circle;
		margin-bottom: 0;
	}
	.release-notes :global(li + li) {
		margin-top: 0.25em;
	}
	.release-notes :global(li > p) {
		margin: 0;
	}
	.release-notes :global(a) {
		color: var(--primary);
		text-decoration: none;
	}
	.release-notes :global(a:hover) {
		text-decoration: underline;
		text-underline-offset: 2px;
	}
	.release-notes :global(code) {
		background: var(--muted);
		color: var(--foreground);
		padding: 0.2em 0.4em;
		border-radius: 0.375rem;
		font-size: 0.85em;
		font-family: ui-monospace, SFMono-Regular, monospace;
	}
	.release-notes :global(pre) {
		background: var(--muted);
		padding: 0.75rem 1rem;
		border-radius: 0.375rem;
		overflow-x: auto;
		margin: 0 0 0.75rem;
	}
	.release-notes :global(pre code) {
		background: transparent;
		padding: 0;
	}
	.release-notes :global(blockquote) {
		border-left: 0.25em solid var(--border);
		padding: 0 1em;
		color: var(--muted-foreground);
		margin: 0 0 0.75rem;
	}
	.release-notes :global(blockquote > :last-child) {
		margin-bottom: 0;
	}
	.release-notes :global(table) {
		display: block;
		max-width: 100%;
		overflow-x: auto;
		border-collapse: collapse;
		margin: 0 0 0.75rem;
	}
	.release-notes :global(th),
	.release-notes :global(td) {
		border: 1px solid var(--border);
		padding: 0.375rem 0.8rem;
	}
	.release-notes :global(th) {
		font-weight: 600;
	}
	.release-notes :global(img) {
		max-width: 100%;
	}
	.release-notes :global(hr) {
		border: 0;
		border-top: 1px solid var(--border);
		margin: 1.25rem 0;
	}
	.release-notes :global(strong) {
		font-weight: 600;
	}
	.release-notes :global(.markdown-alert) {
		--alert-color: var(--info);
		border-left: 0.25em solid var(--alert-color);
		padding: 0.5rem 1rem;
		margin: 0 0 0.75rem;
	}
	.release-notes :global(.markdown-alert > :last-child) {
		margin-bottom: 0;
	}
	.release-notes :global(.markdown-alert-title) {
		display: flex;
		align-items: center;
		gap: 0.5rem;
		font-weight: 500;
		color: var(--alert-color);
		margin-bottom: 0.25rem;
	}
	.release-notes :global(.markdown-alert-title svg) {
		fill: currentColor;
		flex: none;
	}
	.release-notes :global(.markdown-alert-tip) {
		--alert-color: var(--success);
	}
	.release-notes :global(.markdown-alert-important) {
		--alert-color: var(--primary);
	}
	.release-notes :global(.markdown-alert-warning) {
		--alert-color: var(--warning);
	}
	.release-notes :global(.markdown-alert-caution) {
		--alert-color: var(--destructive);
	}
</style>
