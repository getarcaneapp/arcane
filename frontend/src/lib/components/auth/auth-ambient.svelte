<script lang="ts">
	let { showMotion = true }: { showMotion?: boolean } = $props();
</script>

<div class="ambient" aria-hidden="true">
	<div class="ambient__mesh"></div>
	{#if showMotion}
		<div class="ambient__shimmer"></div>
	{/if}
	<div class="ambient__grid"></div>
	<div class="ambient__noise"></div>
	<div class="ambient__vignette"></div>
</div>

<style>
	.ambient {
		position: fixed;
		inset: 0;
		z-index: var(--arcane-z-content);
		overflow: hidden;
		pointer-events: none;
		background: var(--background);
		contain: strict;
	}

	.ambient__mesh {
		position: absolute;
		inset: -20%;
		background:
			radial-gradient(ellipse 60% 50% at 18% 22%, color-mix(in oklab, var(--primary) 14%, transparent) 0%, transparent 60%),
			radial-gradient(ellipse 55% 45% at 82% 78%, color-mix(in oklab, var(--primary) 10%, transparent) 0%, transparent 65%),
			radial-gradient(ellipse 45% 55% at 78% 18%, color-mix(in oklab, var(--primary) 8%, transparent) 0%, transparent 60%),
			radial-gradient(ellipse 50% 40% at 22% 82%, color-mix(in oklab, var(--primary) 6%, transparent) 0%, transparent 65%);
		background-repeat: no-repeat;
		filter: saturate(1);
		opacity: 0.5;
	}

	.ambient__shimmer {
		position: absolute;
		top: 50%;
		left: 50%;
		width: 300vmax;
		height: 300vmax;
		background: conic-gradient(
			from 0deg,
			transparent 0deg,
			color-mix(in oklab, var(--primary) 8%, transparent) 60deg,
			transparent 120deg,
			color-mix(in oklab, var(--primary) 5%, transparent) 200deg,
			transparent 280deg,
			color-mix(in oklab, var(--primary) 7%, transparent) 340deg,
			transparent 360deg
		);
		opacity: 0.4;
		will-change: transform;
		transform: translate3d(-50%, -50%, 0) rotate(0deg);
		transform-origin: center center;
		animation: shimmerRotate 60s linear infinite;
	}

	@keyframes shimmerRotate {
		from {
			transform: translate3d(-50%, -50%, 0) rotate(0deg);
		}
		to {
			transform: translate3d(-50%, -50%, 0) rotate(360deg);
		}
	}

	.ambient__grid {
		position: absolute;
		inset: 0;
		background-image: url("data:image/svg+xml,%3Csvg width='48' height='48' viewBox='0 0 48 48' xmlns='http://www.w3.org/2000/svg'%3E%3Cpath d='M0 0h48v48H0z' fill='none'/%3E%3Cpath d='M47.5 0v48M0 47.5h48' stroke='rgba(150, 150, 150, 1)' stroke-width='1' stroke-opacity='0.15'/%3E%3C/svg%3E");
		background-repeat: repeat;
		background-size: 48px 48px;
		mask-image: radial-gradient(circle at 50% 50%, #000 30%, transparent 80%);
		-webkit-mask-image: radial-gradient(circle at 50% 50%, #000 30%, transparent 80%);
		opacity: 0.5;
	}

	.ambient__noise {
		position: absolute;
		inset: 0;
		opacity: 0.05;
		background-image: url("data:image/svg+xml;utf8,<svg viewBox='0 0 200 200' xmlns='http://www.w3.org/2000/svg'><filter id='n'><feTurbulence type='fractalNoise' baseFrequency='0.9' numOctaves='2' stitchTiles='stitch'/><feColorMatrix values='0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 1 0'/></filter><rect width='100%' height='100%' filter='url(%23n)'/></svg>");
	}

	.ambient__vignette {
		position: absolute;
		inset: 0;
		background: radial-gradient(ellipse at center, transparent 40%, color-mix(in oklab, var(--background) 80%, transparent) 100%);
	}

	@media (prefers-reduced-motion: reduce) {
		.ambient__shimmer {
			animation: none;
		}
	}
</style>
