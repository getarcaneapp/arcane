<script lang="ts">
	import { m } from '#lib/paraglide/messages.js';

	interface Props {
		active?: boolean;
		delay?: number;
		minDuration?: number;
		thickness?: string;
		class?: string;
	}

	let { active = false, delay = 80, minDuration = 350, thickness = 'h-2', class: className = '' }: Props = $props();

	let visible = $state(false);
	let startedAt = 0;
	let showTO: ReturnType<typeof setTimeout> | undefined;
	let hideTO: ReturnType<typeof setTimeout> | undefined;

	function clearTimers() {
		clearTimeout(showTO);
		clearTimeout(hideTO);
	}

	$effect(() => {
		clearTimers();
		if (active) {
			showTO = setTimeout(() => {
				visible = true;
				startedAt = performance.now();
			}, delay);
		} else {
			const elapsed = performance.now() - startedAt;
			const remaining = Math.max(0, minDuration - elapsed);
			hideTO = setTimeout(() => (visible = false), remaining);
		}
		return clearTimers;
	});
</script>

{#if visible}
	<div
		class={`pointer-events-none fixed inset-x-0 top-0 z-(--arcane-z-loading) ${thickness} ${className}`}
		role="progressbar"
		aria-busy="true"
		aria-label={m.common_loading()}
	>
		<div class="track absolute inset-0"></div>
		<div class="packet absolute inset-y-0"></div>
		<span class="sr-only">{m.common_loading()}</span>
	</div>
{/if}

<style>
	.track {
		background: color-mix(in oklab, var(--primary) 18%, transparent);
		backdrop-filter: blur(6px);
	}

	.packet {
		--packet-width: max(6rem, 22%);
		left: 0;
		width: var(--packet-width);
		border-radius: 9999px;
		background: linear-gradient(
			to right,
			color-mix(in oklab, var(--primary) 0%, transparent),
			color-mix(in oklab, var(--primary) 90%, transparent) 30%,
			color-mix(in oklab, var(--primary) 90%, transparent) 70%,
			color-mix(in oklab, var(--primary) 0%, transparent)
		);
		box-shadow:
			0 0 10px color-mix(in oklab, var(--primary) 55%, transparent),
			0 2px 18px color-mix(in oklab, var(--primary) 30%, transparent);
		animation: arcane-bounce 1.8s cubic-bezier(0.15, 0.6, 0.85, 0.4) infinite alternate;
	}

	@keyframes arcane-bounce {
		0% {
			left: 0;
			transform: scaleX(0.55);
		}
		50% {
			transform: scaleX(1.1);
		}
		100% {
			left: calc(100% - var(--packet-width));
			transform: scaleX(0.55);
		}
	}

	@media (prefers-reduced-motion: reduce) {
		.packet {
			animation-duration: 3s;
		}
	}
</style>
