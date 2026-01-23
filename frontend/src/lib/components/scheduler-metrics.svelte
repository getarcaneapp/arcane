<script lang="ts">
	import * as Card from '$lib/components/ui/card';
	import type { GoroutineStats, RuntimeStats } from '$lib/types/system-stats.type';
	import { UsersIcon, CpuIcon, LayersIcon, InfoIcon, MemoryStickIcon, ClockIcon } from '$lib/icons';
	import bytes from 'bytes';
	import { formatDistanceToNow } from 'date-fns';

	let {
		stats,
		threads,
		runtime,
		loading = false
	} = $props<{
		stats?: GoroutineStats;
		threads?: number;
		runtime?: RuntimeStats;
		loading?: boolean;
	}>();

	const maxSafeJS = 9007199254740991;
	const safeCount = (val?: number) => (typeof val === 'number' && Number.isFinite(val) && val >= 0 && val <= maxSafeJS ? val : 0);

	const safeStats = $derived.by(() => {
		const running = safeCount(stats?.running);
		const runnable = safeCount(stats?.runnable);
		const waiting = safeCount(stats?.waiting);
		const syscall = safeCount(stats?.syscall);
		const idle = safeCount(stats?.idle);
		const created = safeCount(stats?.created);
		const total = safeCount(stats?.total) || running + runnable + waiting + syscall + idle;
		return { running, runnable, waiting, syscall, idle, created, total };
	});

	const safeThreads = $derived(safeCount(threads));

	const goroutineItems = $derived([
		{ label: 'Running', value: safeStats.running, color: 'bg-green-500' },
		{ label: 'Runnable', value: safeStats.runnable, color: 'bg-blue-500' },
		{ label: 'Waiting', value: safeStats.waiting, color: 'bg-yellow-500' },
		{ label: 'Syscall', value: safeStats.syscall, color: 'bg-purple-500' },
		{ label: 'Idle', value: safeStats.idle, color: 'bg-gray-500' }
	]);

	const formatBytes = (val: number) => bytes.format(val, { unitSeparator: ' ' }) ?? '0 B';

	const memoryStats = $derived([
		{ label: 'Heap Allocated', value: formatBytes(runtime?.heapAlloc ?? 0), desc: 'Memory currently used by heap objects' },
		{ label: 'Heap In-use', value: formatBytes(runtime?.heapInuse ?? 0), desc: 'Memory in in-use spans' },
		{ label: 'Heap Idle', value: formatBytes(runtime?.heapIdle ?? 0), desc: 'Memory in idle spans' },
		{ label: 'Stack In-use', value: formatBytes(runtime?.stackInuse ?? 0), desc: 'Memory used by goroutine stacks' },
		{ label: 'MSpan In-use', value: formatBytes(runtime?.mSpanInuse ?? 0), desc: 'Memory used by internal mspan structures' },
		{ label: 'MCache In-use', value: formatBytes(runtime?.mCacheInuse ?? 0), desc: 'Memory used by internal mcache structures' }
	]);

	const gcStats = $derived([
		{ label: 'Next GC Target', value: formatBytes(runtime?.nextGC ?? 0), desc: 'Target heap size for the next GC cycle' },
		{
			label: 'GC CPU Fraction',
			value: `${((runtime?.gcCPUFraction ?? 0) * 100).toFixed(4)}%`,
			desc: 'Fraction of total available CPU time used by GC'
		},
		{ label: 'Total GC Cycles', value: runtime?.numGC?.toLocaleString() ?? '0', desc: 'Total number of completed GC cycles' },
		{
			label: 'Forced GC Cycles',
			value: runtime?.numForcedGC?.toLocaleString() ?? '0',
			desc: 'Number of GC cycles forced by the user'
		}
	]);
</script>

<div class="space-y-6">
	<div class="grid grid-cols-1 gap-6 md:grid-cols-2">
		<Card.Root class="border-border/70 bg-surface/40 shadow-[0_10px_30px_rgba(0,0,0,0.35)]">
			<Card.Header class="border-border/60 bg-muted/20 border-b pb-4">
				<div class="flex items-center justify-between">
					<Card.Title class="text-sm font-medium">Scheduler</Card.Title>
					<CpuIcon class="text-muted-foreground size-4" />
				</div>
				<Card.Description class="text-xs">Goroutine states and OS threads</Card.Description>
			</Card.Header>
			<Card.Content class="py-4">
				{#if loading}
					<div class="space-y-4">
						<div class="grid grid-cols-2 gap-4">
							<div class="bg-muted h-10 animate-pulse rounded"></div>
							<div class="bg-muted h-10 animate-pulse rounded"></div>
						</div>
					</div>
				{:else}
					<div class="grid grid-cols-2 gap-4">
						<div class="border-border/60 bg-muted/20 rounded-lg border px-3 py-2">
							<span class="text-muted-foreground text-[10px] font-semibold tracking-wider uppercase">Total Goroutines</span>
							<div class="mt-1 flex items-center gap-2">
								<UsersIcon class="text-primary size-4" />
								<span class="text-lg font-bold tabular-nums">{safeStats.total.toLocaleString()}</span>
							</div>
						</div>
						<div class="border-border/60 bg-muted/20 rounded-lg border px-3 py-2">
							<span class="text-muted-foreground text-[10px] font-semibold tracking-wider uppercase">OS Threads</span>
							<div class="mt-1 flex items-center gap-2">
								<CpuIcon class="text-primary size-4" />
								<span class="text-lg font-bold tabular-nums">{safeThreads.toLocaleString()}</span>
							</div>
						</div>
					</div>

					<div class="mt-6 space-y-4">
						<div class="border-border/60 bg-muted/30 flex h-3 w-full overflow-hidden rounded-full border shadow-inner">
							{#each goroutineItems as item}
								{#if safeStats.total > 0}
									<div
										class={item.color}
										style="width: {(item.value / safeStats.total) * 100}%"
										title="{item.label}: {item.value}"
									></div>
								{/if}
							{/each}
						</div>

						<div class="grid grid-cols-2 gap-3 sm:grid-cols-3">
							{#each goroutineItems as item}
								<div class="border-border/60 bg-muted/10 flex items-center gap-2 rounded-md border px-2 py-1">
									<div class="size-2 shrink-0 rounded-full {item.color}"></div>
									<div class="flex min-w-0 flex-col">
										<span class="text-muted-foreground truncate text-[10px] font-semibold tracking-wider uppercase">
											{item.label}
										</span>
										<span class="text-xs font-medium tabular-nums">{item.value.toLocaleString()}</span>
									</div>
								</div>
							{/each}
							<div class="border-border/60 bg-muted/10 flex items-center gap-2 rounded-md border px-2 py-1">
								<LayersIcon class="text-muted-foreground size-2 shrink-0" />
								<div class="flex min-w-0 flex-col">
									<span class="text-muted-foreground truncate text-[10px] font-semibold tracking-wider uppercase"> Created </span>
									<span class="text-xs font-medium tabular-nums">{safeStats.created.toLocaleString()}</span>
								</div>
							</div>
						</div>
					</div>
				{/if}
			</Card.Content>
		</Card.Root>

		<Card.Root class="border-border/70 bg-surface/40 shadow-[0_10px_30px_rgba(0,0,0,0.35)]">
			<Card.Header class="border-border/60 bg-muted/20 border-b pb-4">
				<div class="flex items-center justify-between">
					<Card.Title class="text-sm font-medium">Memory Management</Card.Title>
					<MemoryStickIcon class="text-muted-foreground size-4" />
				</div>
				<Card.Description class="text-xs">Go runtime memory allocation</Card.Description>
			</Card.Header>
			<Card.Content class="py-4">
				{#if loading}
					<div class="bg-muted h-32 animate-pulse rounded"></div>
				{:else}
					<div class="grid grid-cols-2 gap-4">
						{#each memoryStats as item}
							<div class="border-border/60 bg-muted/20 rounded-lg border p-3">
								<span class="text-muted-foreground text-[10px] font-semibold tracking-wider uppercase" title={item.desc}>
									{item.label}
								</span>
								<span class="text-sm font-bold tabular-nums">{item.value}</span>
							</div>
						{/each}
					</div>
				{/if}
			</Card.Content>
		</Card.Root>
	</div>

	<Card.Root class="border-border/70 bg-surface/40 shadow-[0_10px_30px_rgba(0,0,0,0.35)]">
		<Card.Header class="border-border/60 bg-muted/20 border-b pb-4">
			<div class="flex items-center justify-between">
				<Card.Title class="text-sm font-medium">Garbage Collection</Card.Title>
				<InfoIcon class="text-muted-foreground size-4" />
			</div>
			<Card.Description class="text-xs">GC performance and timing</Card.Description>
		</Card.Header>
		<Card.Content class="py-4">
			{#if loading}
				<div class="bg-muted h-24 animate-pulse rounded"></div>
			{:else}
				<div class="grid grid-cols-1 gap-4 sm:grid-cols-2 lg:grid-cols-4">
					{#each gcStats as item}
						<div class="border-border/60 bg-muted/20 rounded-lg border p-4">
							<span class="text-muted-foreground block text-[10px] font-semibold tracking-wider uppercase" title={item.desc}>
								{item.label}
							</span>
							<span class="text-base font-bold tabular-nums">{item.value}</span>
						</div>
					{/each}
				</div>

				<div class="border-border/60 text-muted-foreground mt-6 flex items-center gap-2 border-t pt-4 text-xs">
					<ClockIcon class="size-3" />
					<span>Last GC: {runtime?.lastGC ? formatDistanceToNow(new Date(runtime.lastGC / 1000000)) + ' ago' : 'Never'}</span>
				</div>
			{/if}
		</Card.Content>
	</Card.Root>
</div>
