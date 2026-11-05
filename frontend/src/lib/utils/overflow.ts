import type { Attachment } from 'svelte/attachments';

export type ScrollOverflow = { below: boolean; left: boolean; right: boolean; scrollable: boolean };

export function trackOverflow(onChange: (overflow: ScrollOverflow) => void): Attachment<HTMLElement> {
	return (node) => {
		const measure = () =>
			onChange({
				below: node.scrollHeight - node.scrollTop - node.clientHeight > 1,
				left: node.scrollLeft > 1,
				right: node.scrollWidth - node.scrollLeft - node.clientWidth > 1,
				scrollable: node.scrollHeight - node.clientHeight > 1
			});
		const resizes = new ResizeObserver(measure);
		const observeAll = () => {
			resizes.disconnect();
			resizes.observe(node);
			for (const child of node.children) resizes.observe(child);
		};
		const mutations = new MutationObserver(() => {
			observeAll();
			measure();
		});
		mutations.observe(node, { childList: true, subtree: true });
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
