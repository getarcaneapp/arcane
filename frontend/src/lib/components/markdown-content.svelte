<script lang="ts" module>
	let renderer: Promise<(source: string) => string> | undefined;

	// marked and DOMPurify load on first render so they stay out of the app shell bundle.
	function loadRenderer() {
		renderer ??= Promise.all([import('marked'), import('marked-alert'), import('isomorphic-dompurify')])
			.then(([{ Marked }, { default: markedAlert }, { default: DOMPurify }]) => {
				const marked = new Marked(markedAlert());
				// External links open in a new tab so they never navigate away from unsaved edits.
				DOMPurify.addHook('afterSanitizeAttributes', (node) => {
					if (node.localName !== 'a' && node.localName !== 'area') return;
					const href = node.getAttribute('href')?.trim();
					if (!href || href.startsWith('#')) return;
					node.setAttribute('target', '_blank');
					node.setAttribute('rel', 'noopener noreferrer');
				});
				return (source: string): string => {
					if (!source) return '';
					try {
						const parsed = marked.parse(source, { async: false }) as string;
						return DOMPurify.sanitize(parsed, {
							FORBID_TAGS: ['style', 'form', 'button', 'textarea', 'select'],
							// Popovers render in the top layer, which paint containment cannot clip.
							FORBID_ATTR: ['style', 'popover', 'popovertarget', 'popovertargetaction']
						});
					} catch {
						return '';
					}
				};
			})
			.catch((error: unknown) => {
				// Drop the failed load so the next instance retries it.
				renderer = undefined;
				throw error;
			});
		return renderer;
	}
</script>

<script lang="ts">
	import { m } from '#lib/paraglide/messages.js';
	import { cn } from '#lib/utils.js';

	let { markdown, class: className }: { markdown: string; class?: string } = $props();

	const renderMarkdown = loadRenderer();
</script>

{#await renderMarkdown then render}
	{@const html = render(markdown)}
	{#if html}
		<div class={cn('markdown-content text-sm leading-relaxed', className)}>
			<!-- eslint-disable-next-line svelte/no-at-html-tags -->
			{@html html}
		</div>
	{/if}
{:catch}
	<p class="text-sm text-destructive">{m.markdown_render_failed()}</p>
{/await}

<style>
	/* Paint containment clips fixed or absolute descendants built from untrusted classes; it does not clip the top layer. */
	.markdown-content {
		contain: paint;
		overflow-wrap: break-word;
	}
	.markdown-content :global(> :first-child) {
		margin-top: 0;
	}
	.markdown-content :global(> :last-child) {
		margin-bottom: 0;
	}
	.markdown-content :global(h1),
	.markdown-content :global(h2),
	.markdown-content :global(h3),
	.markdown-content :global(h4) {
		font-weight: 600;
		line-height: 1.25;
		margin-top: 1.25rem;
		margin-bottom: 0.75rem;
		color: var(--foreground);
	}
	.markdown-content :global(h1),
	.markdown-content :global(h2) {
		padding-bottom: 0.3em;
		border-bottom: 1px solid var(--border);
	}
	.markdown-content :global(h1) {
		font-size: 1.2rem;
	}
	.markdown-content :global(h2) {
		font-size: 1.05rem;
	}
	.markdown-content :global(h3) {
		font-size: 0.95rem;
	}
	.markdown-content :global(h4) {
		font-size: 0.875rem;
	}
	.markdown-content :global(p) {
		margin: 0 0 0.75rem;
	}
	.markdown-content :global(ul),
	.markdown-content :global(ol) {
		margin: 0 0 0.75rem;
		padding-left: 1.5rem;
	}
	.markdown-content :global(ul) {
		list-style: disc;
	}
	.markdown-content :global(ol) {
		list-style: decimal;
	}
	.markdown-content :global(ul ul) {
		list-style: circle;
		margin-bottom: 0;
	}
	.markdown-content :global(li + li) {
		margin-top: 0.25em;
	}
	.markdown-content :global(li > p) {
		margin: 0;
	}
	.markdown-content :global(a) {
		color: var(--primary);
		text-decoration: none;
	}
	.markdown-content :global(a:hover) {
		text-decoration: underline;
		text-underline-offset: 2px;
	}
	.markdown-content :global(code) {
		background: var(--muted);
		color: var(--foreground);
		padding: 0.2em 0.4em;
		border-radius: 0.375rem;
		font-size: 0.85em;
		font-family: ui-monospace, SFMono-Regular, monospace;
	}
	.markdown-content :global(pre) {
		background: var(--muted);
		padding: 0.75rem 1rem;
		border-radius: 0.375rem;
		overflow-x: auto;
		margin: 0 0 0.75rem;
	}
	.markdown-content :global(pre code) {
		background: transparent;
		padding: 0;
	}
	.markdown-content :global(blockquote) {
		border-left: 0.25em solid var(--border);
		padding: 0 1em;
		color: var(--muted-foreground);
		margin: 0 0 0.75rem;
	}
	.markdown-content :global(blockquote > :last-child) {
		margin-bottom: 0;
	}
	.markdown-content :global(table) {
		display: block;
		max-width: 100%;
		overflow-x: auto;
		border-collapse: collapse;
		margin: 0 0 0.75rem;
	}
	.markdown-content :global(th),
	.markdown-content :global(td) {
		border: 1px solid var(--border);
		padding: 0.375rem 0.8rem;
	}
	.markdown-content :global(th) {
		font-weight: 600;
	}
	.markdown-content :global(img) {
		max-width: 100%;
	}
	.markdown-content :global(hr) {
		border: 0;
		border-top: 1px solid var(--border);
		margin: 1.25rem 0;
	}
	.markdown-content :global(strong) {
		font-weight: 600;
	}
	.markdown-content :global(.markdown-alert) {
		--alert-color: var(--info);
		border-left: 0.25em solid var(--alert-color);
		padding: 0.5rem 1rem;
		margin: 0 0 0.75rem;
	}
	.markdown-content :global(.markdown-alert > :last-child) {
		margin-bottom: 0;
	}
	.markdown-content :global(.markdown-alert-title) {
		display: flex;
		align-items: center;
		gap: 0.5rem;
		font-weight: 500;
		color: var(--alert-color);
		margin-bottom: 0.25rem;
	}
	.markdown-content :global(.markdown-alert-title svg) {
		fill: currentColor;
		flex: none;
	}
	.markdown-content :global(.markdown-alert-tip) {
		--alert-color: var(--success);
	}
	.markdown-content :global(.markdown-alert-important) {
		--alert-color: var(--primary);
	}
	.markdown-content :global(.markdown-alert-warning) {
		--alert-color: var(--warning);
	}
	.markdown-content :global(.markdown-alert-caution) {
		--alert-color: var(--destructive);
	}
</style>
