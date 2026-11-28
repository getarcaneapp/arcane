import type { Attachment } from 'svelte/attachments';

export type ScrollOverflow = { below: boolean; left: boolean; right: boolean; scrollable: boolean };

export function trackOverflow(onChange: (overflow: ScrollOverflow) => void): Attachment<HTMLElement> {
	return (node) => {
		const measure = () =>
			onChange({
				below: node.scrollHeight - node.scrollTop - node.clientHeight > 4,
				left: node.scrollLeft > 1,
				right: node.scrollWidth - node.scrollLeft - node.clientWidth > 1,
				scrollable: node.scrollHeight - node.clientHeight > 4
			});
		const resizes = new ResizeObserver(measure);
		const observeAll = () => {
			resizes.disconnect();
			resizes.observe(node);
			for (const child of node.children) resizes.observe(child);
		};
		// Re-observing fires the ResizeObserver, which measures after layout instead of forcing one per mutation.
		const mutations = new MutationObserver(observeAll);
		mutations.observe(node, { childList: true });
		observeAll();
		const frame = requestAnimationFrame(measure);
		node.addEventListener('scroll', measure, { passive: true });
		return () => {
			cancelAnimationFrame(frame);
			node.removeEventListener('scroll', measure);
			mutations.disconnect();
			resizes.disconnect();
		};
	};
}
