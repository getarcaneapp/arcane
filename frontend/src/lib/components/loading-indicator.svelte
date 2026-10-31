<script lang="ts">
	import { m } from '#lib/paraglide/messages.js';

	interface Props {
		active?: boolean;
		thickness?: string;
		class?: string;
	}

	let { active = false, thickness = 'h-2', class: className = '' }: Props = $props();
</script>

<div
	class={`loading pointer-events-none fixed inset-x-0 top-0 z-(--arcane-z-loading) ${thickness} ${className}`}
	class:active
	role="progressbar"
	aria-hidden={!active}
	aria-busy={active}
	aria-label={m.common_loading()}
>
	<div class="track absolute inset-0"></div>
	<div class="wave absolute inset-0"></div>
	<div class="wave absolute inset-0"></div>
	<div class="wave absolute inset-0"></div>
	{#if active}
		<span class="sr-only">{m.common_loading()}</span>
	{/if}
</div>

<style>
	.loading {
		opacity: 0;
		visibility: hidden;
		transition:
			opacity 250ms ease-out 100ms,
			visibility 0s linear 350ms;
	}

	.loading.active {
		opacity: 1;
		visibility: visible;
		transition:
			opacity 120ms ease-out,
			visibility 0s;
	}

	.track {
		background: color-mix(in oklab, var(--primary) 18%, transparent);
		backdrop-filter: blur(6px);
	}

	.wave {
		transform-origin: center;
		transform: scaleX(0);
		opacity: 0;
		background: linear-gradient(
			to right,
			color-mix(in oklab, var(--primary) 0%, transparent),
			color-mix(in oklab, var(--primary) 90%, transparent) 8%,
			color-mix(in oklab, var(--primary) 0%, transparent) 26%,
			color-mix(in oklab, var(--primary) 0%, transparent) 74%,
			color-mix(in oklab, var(--primary) 90%, transparent) 92%,
			color-mix(in oklab, var(--primary) 0%, transparent)
		);
		filter: drop-shadow(0 0 6px color-mix(in oklab, var(--primary) 55%, transparent));
		will-change: transform, opacity;
		animation: arcane-wave 1.8s cubic-bezier(0.2, 0.6, 0.35, 1) infinite;
		animation-play-state: paused;
	}

	.wave:nth-of-type(3) {
		animation-delay: 0.6s;
	}

	.wave:nth-of-type(4) {
		animation-delay: 1.2s;
	}

	.loading.active .wave {
		animation-play-state: running;
	}

	@keyframes arcane-wave {
		0% {
			transform: scaleX(0);
			opacity: 0;
		}
		15% {
			opacity: 1;
		}
		70% {
			opacity: 0.85;
		}
		100% {
			transform: scaleX(1);
			opacity: 0;
		}
	}

	@media (prefers-reduced-motion: reduce) {
		.wave {
			animation-duration: 3.6s;
		}
	}
</style>
