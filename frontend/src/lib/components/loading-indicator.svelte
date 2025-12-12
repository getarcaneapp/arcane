<script lang="ts">
	import { m } from '$lib/paraglide/messages';

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
				startedAt = Date.now();
			}, delay);
		} else {
			const elapsed = Date.now() - startedAt;
			const remaining = Math.max(0, minDuration - elapsed);
			hideTO = setTimeout(() => (visible = false), remaining);
		}
		return clearTimers;
	});
</script>

{#if visible}
	<div
		class={`pointer-events-none fixed inset-x-0 top-0 z-[9999] ${thickness} ${className}`}
		role="progressbar"
		aria-busy="true"
		aria-label={m.common_loading()}
	>
		<div class="relative h-full w-full overflow-hidden">
			<div class="bg-primary/25 absolute inset-0"></div>

			<div class="bar absolute inset-y-0 left-0 w-1/3">
				<div class="peg absolute top-0 right-0 h-full w-3"></div>
			</div>

			<div class="bar absolute inset-y-0 left-0 w-1/3" style="animation-delay: -1s;">
				<div class="peg absolute top-0 right-0 h-full w-3"></div>
			</div>

			<span class="sr-only">{m.common_loading()}</span>
		</div>
	</div>
{/if}

<style>
	:root {
		--arcane-primary: hsl(var(--primary));
	}

	.bar {
		background: var(--arcane-primary);
		animation: arcane-slide 2s linear infinite;
		box-shadow:
			0 0 12px var(--arcane-primary),
			0 0 2px var(--arcane-primary);
		opacity: 0.95;
	}

	.peg {
		background: linear-gradient(
			to right,
			color-mix(in oklab, var(--arcane-primary) 0%, transparent),
			color-mix(in oklab, var(--arcane-primary) 90%, transparent)
		);
		filter: blur(2px);
	}

	@keyframes arcane-slide {
		0% {
			transform: translateX(-100%);
		}
		100% {
			transform: translateX(300%);
		}
	}

	@media (prefers-reduced-motion: reduce) {
		.bar {
			animation-duration: 4s;
		}
	}
</style>
