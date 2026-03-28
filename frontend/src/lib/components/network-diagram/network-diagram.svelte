<script lang="ts">
	import { browser } from '$app/environment';
	import { goto } from '$app/navigation';
	import StatusBadge from '$lib/components/badges/status-badge.svelte';
	import { m } from '$lib/paraglide/messages';
	import type { NetworkTopologyDto, TopologyEdgeDto, TopologyNodeDto } from '$lib/types/network.type';
	import { cn } from '$lib/utils';
	import { mode } from 'mode-watcher';
	import { onMount } from 'svelte';
	import { Background, BackgroundVariant, Controls, MiniMap, Position, SvelteFlow, type Edge, type Node } from '@xyflow/svelte';
	import '@xyflow/svelte/dist/style.css';

	let {
		topology,
		class: className = ''
	}: {
		topology: NetworkTopologyDto;
		class?: string;
	} = $props();

	let isReady = $state(false);

	onMount(() => {
		isReady = true;
	});

	type DiagramNodeData = {
		href: string;
		kind: 'network' | 'container';
		label: string;
	};

	type NodePalette = {
		background: string;
		surface: string;
		border: string;
		text: string;
	};

	const networkNodes = $derived(topology.nodes.filter((node) => node.type === 'network'));
	const containerNodes = $derived(topology.nodes.filter((node) => node.type === 'container'));
	const isDarkMode = $derived(mode.current === 'dark');

	const canvasTheme = $derived(
		isDarkMode
			? {
					flowBackground: '#0b1220',
					edgeLabel: '#cbd5e1',
					edgeLabelBackground: 'rgba(15, 23, 42, 0.92)',
					edgeStroke: 'rgba(148, 163, 184, 0.42)',
					backgroundDots: 'rgba(148, 163, 184, 0.22)',
					minimapBackground: 'rgba(15, 23, 42, 0.92)',
					minimapMask: 'rgba(15, 23, 42, 0.45)',
					minimapMaskStroke: 'rgba(148, 163, 184, 0.34)',
					shellBackground:
						'radial-gradient(circle at top left, rgba(14, 165, 233, 0.14), transparent 28%), radial-gradient(circle at top right, rgba(124, 58, 237, 0.16), transparent 24%), linear-gradient(180deg, rgba(12, 18, 32, 0.96), rgba(8, 12, 24, 0.98))',
					shellBorder: 'color-mix(in srgb, var(--border) 82%, transparent)',
					shellShadow: '0 24px 72px rgba(2, 6, 23, 0.5)',
					nodeShadow: '0 18px 52px rgba(2, 6, 23, 0.42)',
					nodeHoverShadow: '0 28px 64px rgba(2, 6, 23, 0.54)',
					nodeSelectedShadow: '0 0 0 3px rgba(56, 189, 248, 0.22)',
					controlsBackground: 'rgba(15, 23, 42, 0.92)',
					controlsForeground: 'rgb(226, 232, 240)',
					controlsBorder: 'rgba(148, 163, 184, 0.18)',
					controlsShadow: '0 14px 32px rgba(2, 6, 23, 0.36)',
					minimapBorder: 'rgba(148, 163, 184, 0.18)',
					minimapShadow: '0 14px 32px rgba(2, 6, 23, 0.36)'
				}
			: {
					flowBackground: '#f8fafc',
					edgeLabel: '#475569',
					edgeLabelBackground: 'rgba(255, 255, 255, 0.96)',
					edgeStroke: 'rgba(71, 85, 105, 0.55)',
					backgroundDots: 'rgba(100, 116, 139, 0.18)',
					minimapBackground: 'rgba(248, 250, 252, 0.95)',
					minimapMask: 'rgba(15, 23, 42, 0.12)',
					minimapMaskStroke: 'rgba(15, 23, 42, 0.28)',
					shellBackground:
						'radial-gradient(circle at top left, rgba(125, 211, 252, 0.18), transparent 28%), radial-gradient(circle at top right, rgba(196, 181, 253, 0.2), transparent 24%), linear-gradient(180deg, rgba(248, 250, 252, 0.96), rgba(255, 255, 255, 0.98))',
					shellBorder: 'color-mix(in srgb, var(--border) 72%, transparent)',
					shellShadow: '0 20px 60px rgba(15, 23, 42, 0.08)',
					nodeShadow: '0 16px 40px rgba(15, 23, 42, 0.08)',
					nodeHoverShadow: '0 24px 56px rgba(15, 23, 42, 0.12)',
					nodeSelectedShadow: '0 0 0 3px rgba(56, 189, 248, 0.2)',
					controlsBackground: 'rgba(255, 255, 255, 0.96)',
					controlsForeground: 'rgb(51, 65, 85)',
					controlsBorder: 'rgba(148, 163, 184, 0.24)',
					controlsShadow: '0 12px 28px rgba(15, 23, 42, 0.12)',
					minimapBorder: 'rgba(148, 163, 184, 0.24)',
					minimapShadow: '0 12px 28px rgba(15, 23, 42, 0.12)'
				}
	);

	function networkPalette(driver?: string, darkMode = false): NodePalette {
		if (darkMode) {
			switch ((driver ?? '').toLowerCase()) {
				case 'bridge':
					return { background: '#083344', surface: '#0f172a', border: '#22d3ee', text: '#cffafe' };
				case 'overlay':
					return { background: '#3b0764', surface: '#111827', border: '#a855f7', text: '#f3e8ff' };
				case 'ipvlan':
					return { background: '#7c2d12', surface: '#111827', border: '#fb923c', text: '#ffedd5' };
				case 'macvlan':
					return { background: '#881337', surface: '#111827', border: '#f472b6', text: '#fce7f3' };
				default:
					return { background: '#1e293b', surface: '#0f172a', border: '#94a3b8', text: '#e2e8f0' };
			}
		}

		switch ((driver ?? '').toLowerCase()) {
			case 'bridge':
				return { background: '#ecfeff', surface: '#ffffff', border: '#0891b2', text: '#155e75' };
			case 'overlay':
				return { background: '#f5f3ff', surface: '#ffffff', border: '#7c3aed', text: '#5b21b6' };
			case 'ipvlan':
				return { background: '#fff7ed', surface: '#ffffff', border: '#ea580c', text: '#9a3412' };
			case 'macvlan':
				return { background: '#fff1f2', surface: '#ffffff', border: '#e11d48', text: '#9f1239' };
			default:
				return { background: '#f8fafc', surface: '#ffffff', border: '#64748b', text: '#334155' };
		}
	}

	function containerPalette(status?: string, darkMode = false): NodePalette {
		if (darkMode) {
			switch ((status ?? '').toLowerCase()) {
				case 'running':
					return { background: '#064e3b', surface: '#0f172a', border: '#34d399', text: '#d1fae5' };
				case 'paused':
					return { background: '#78350f', surface: '#111827', border: '#fbbf24', text: '#fef3c7' };
				case 'exited':
				case 'dead':
					return { background: '#7f1d1d', surface: '#111827', border: '#f87171', text: '#fee2e2' };
				default:
					return { background: '#1e293b', surface: '#0f172a', border: '#94a3b8', text: '#e2e8f0' };
			}
		}

		switch ((status ?? '').toLowerCase()) {
			case 'running':
				return { background: '#ecfdf5', surface: '#ffffff', border: '#10b981', text: '#065f46' };
			case 'paused':
				return { background: '#fffbeb', surface: '#ffffff', border: '#f59e0b', text: '#92400e' };
			case 'exited':
			case 'dead':
				return { background: '#fef2f2', surface: '#ffffff', border: '#ef4444', text: '#991b1b' };
			default:
				return { background: '#f8fafc', surface: '#ffffff', border: '#64748b', text: '#334155' };
		}
	}

	function nodeLabel(node: TopologyNodeDto): string {
		if (node.type === 'network') {
			const suffix = node.metadata.driver ? ` · ${node.metadata.driver}` : '';
			return `${node.name}${suffix}`;
		}

		const status = node.metadata.status ? ` · ${node.metadata.status}` : '';
		return `${node.name}${status}`;
	}

	function nodeTitle(node: TopologyNodeDto): string {
		if (node.type === 'network') {
			return [
				node.name,
				node.metadata.driver ? `Driver: ${node.metadata.driver}` : null,
				node.metadata.scope ? `Scope: ${node.metadata.scope}` : null,
				node.metadata.isDefault ? 'Default Docker network' : null
			]
				.filter(Boolean)
				.join('\n');
		}

		return [node.name, node.metadata.status ? `Status: ${node.metadata.status}` : null, node.metadata.image ?? null]
			.filter(Boolean)
			.join('\n');
	}

	function edgeLabel(edge: TopologyEdgeDto): string | undefined {
		const labels = [edge.ipv4Address, edge.ipv6Address].filter(Boolean);
		if (labels.length === 0) {
			return undefined;
		}
		return labels.join(' | ');
	}

	function containerSourceMap(edges: TopologyEdgeDto[]): Map<string, string[]> {
		const map = new Map<string, string[]>();
		for (const edge of edges) {
			const existing = map.get(edge.target) ?? [];
			existing.push(edge.source);
			map.set(edge.target, existing);
		}
		return map;
	}

	function buildContainerYPositions(
		containers: TopologyNodeDto[],
		edges: TopologyEdgeDto[],
		networkY: Map<string, number>
	): Map<string, number> {
		const positions = new Map<string, number>();
		const sourcesByContainer = containerSourceMap(edges);
		const staged = containers.map((node, index) => {
			const sources = sourcesByContainer.get(node.id) ?? [];
			const connectedY = sources.map((source) => networkY.get(source)).filter((value): value is number => value !== undefined);
			const averageY =
				connectedY.length > 0 ? connectedY.reduce((sum, value) => sum + value, 0) / connectedY.length : index * 180;

			return {
				node,
				averageY
			};
		});

		staged.sort((a, b) => a.averageY - b.averageY || a.node.name.localeCompare(b.node.name));

		let lastY = -180;
		for (const entry of staged) {
			const nextY = Math.max(entry.averageY, lastY + 150);
			positions.set(entry.node.id, nextY);
			lastY = nextY;
		}

		return positions;
	}

	const diagramNodes = $derived.by<Node<DiagramNodeData>[]>(() => {
		const networkY = new Map(networkNodes.map((node, index) => [node.id, index * 180]));
		const containerY = buildContainerYPositions(containerNodes, topology.edges, networkY);

		const graphNodes: Node<DiagramNodeData>[] = [];

		for (const node of networkNodes) {
			const palette = networkPalette(node.metadata.driver, isDarkMode);
			graphNodes.push({
				id: node.id,
				position: { x: 40, y: networkY.get(node.id) ?? 0 },
				sourcePosition: Position.Right,
				targetPosition: Position.Left,
				type: 'default',
				class: 'arcane-topology-node arcane-topology-node-network',
				style: [
					'width: 280px',
					'border-radius: 20px',
					'padding: 18px 20px',
					`border: 1px solid ${palette.border}`,
					`background: linear-gradient(135deg, ${palette.background}, ${palette.surface})`,
					`color: ${palette.text}`,
					`box-shadow: ${canvasTheme.nodeShadow}`,
					'font-size: 13px',
					'font-weight: 600'
				].join('; '),
				data: {
					href: `/networks/${node.id}`,
					kind: 'network',
					label: nodeLabel(node)
				},
				ariaLabel: nodeLabel(node),
				domAttributes: {
					title: nodeTitle(node)
				}
			});
		}

		for (const node of containerNodes) {
			const palette = containerPalette(node.metadata.status, isDarkMode);
			graphNodes.push({
				id: node.id,
				position: { x: 440, y: containerY.get(node.id) ?? 0 },
				data: {
					href: `/containers/${node.id}`,
					kind: 'container',
					label: nodeLabel(node)
				},
				sourcePosition: Position.Right,
				targetPosition: Position.Left,
				type: 'default',
				class: 'arcane-topology-node arcane-topology-node-container',
				style: [
					'width: 300px',
					'border-radius: 20px',
					'padding: 18px 20px',
					`border: 1px solid ${palette.border}`,
					`background: linear-gradient(135deg, ${palette.background}, ${palette.surface})`,
					`color: ${palette.text}`,
					`box-shadow: ${canvasTheme.nodeShadow}`,
					'font-size: 13px',
					'font-weight: 600'
				].join('; '),
				ariaLabel: nodeLabel(node),
				domAttributes: {
					title: nodeTitle(node)
				}
			});
		}

		return graphNodes;
	});

	const diagramEdges = $derived.by<Edge[]>(() =>
		topology.edges.map((edge) => ({
			id: edge.id,
			source: edge.source,
			target: edge.target,
			type: 'smoothstep',
			label: edgeLabel(edge),
			labelStyle: `fill: ${canvasTheme.edgeLabel}; font-size: 11px; font-weight: 600;`,
			style: `stroke: ${canvasTheme.edgeStroke}; stroke-width: 1.5;`,
			interactionWidth: 24,
			selectable: false,
			focusable: false
		}))
	);

	function miniMapColor(node: Node): string {
		const kind = (node.data as DiagramNodeData | undefined)?.kind;
		if (kind === 'network') {
			return '#7c3aed';
		}
		return '#10b981';
	}

	function handleNodeClick({ node }: { node: Node }) {
		const href = (node.data as DiagramNodeData | undefined)?.href;
		if (href) {
			void goto(href);
		}
	}
</script>

<div class={cn('space-y-4', className)}>
	<div class="flex flex-wrap items-center gap-2">
		<StatusBadge text={m.networks_topology_legend_networks()} variant="violet" minWidth="none" />
		<StatusBadge text={m.networks_topology_legend_containers()} variant="emerald" minWidth="none" />
		<p class="text-muted-foreground text-sm">{m.networks_topology_hint()}</p>
	</div>

	{#if topology.nodes.length === 0}
		<div class="bg-card border-border/70 rounded-3xl border px-6 py-16 text-center shadow-sm">
			<p class="text-foreground text-base font-medium">{m.networks_topology_empty()}</p>
		</div>
	{:else if browser && isReady}
		<div
			class="network-diagram-shell overflow-hidden rounded-[28px] border"
			style:--diagram-shell-background={canvasTheme.shellBackground}
			style:--diagram-shell-border={canvasTheme.shellBorder}
			style:--diagram-shell-shadow={canvasTheme.shellShadow}
			style:--diagram-node-hover-shadow={canvasTheme.nodeHoverShadow}
			style:--diagram-node-selected-shadow={canvasTheme.nodeSelectedShadow}
			style:--diagram-controls-background={canvasTheme.controlsBackground}
			style:--diagram-controls-foreground={canvasTheme.controlsForeground}
			style:--diagram-controls-border={canvasTheme.controlsBorder}
			style:--diagram-controls-shadow={canvasTheme.controlsShadow}
			style:--diagram-flow-background={canvasTheme.flowBackground}
			style:--diagram-edge-label-background={canvasTheme.edgeLabelBackground}
			style:--diagram-minimap-border={canvasTheme.minimapBorder}
			style:--diagram-minimap-shadow={canvasTheme.minimapShadow}
		>
			<SvelteFlow
				nodes={diagramNodes}
				edges={diagramEdges}
				colorMode={isDarkMode ? 'dark' : 'light'}
				fitView
				minZoom={0.35}
				maxZoom={1.75}
				panOnScroll
				panOnDrag
				nodesDraggable={false}
				nodesConnectable={false}
				elementsSelectable
				attributionPosition="bottom-left"
				class="network-diagram-flow"
				onnodeclick={handleNodeClick}
			>
				<Controls showLock={false} />
				<MiniMap
					bgColor={canvasTheme.minimapBackground}
					maskColor={canvasTheme.minimapMask}
					maskStrokeColor={canvasTheme.minimapMaskStroke}
					nodeColor={miniMapColor}
					nodeStrokeColor={miniMapColor}
				/>
				<Background variant={BackgroundVariant.Dots} gap={18} size={1.2} bgColor={canvasTheme.backgroundDots} />
			</SvelteFlow>
		</div>
	{/if}
</div>

<style>
	.network-diagram-shell {
		height: calc(100dvh - 12.5rem);
		min-height: 36rem;
		background: var(--diagram-shell-background);
		border-color: var(--diagram-shell-border);
		box-shadow: var(--diagram-shell-shadow);
	}

	:global(.network-diagram-flow .svelte-flow__renderer) {
		background: transparent;
	}

	:global(.network-diagram-flow.svelte-flow) {
		--xy-background-color: var(--diagram-flow-background);
		--xy-edge-label-background-color: var(--diagram-edge-label-background);
		background-color: var(--diagram-flow-background);
	}

	:global(.network-diagram-flow .svelte-flow__background) {
		background-color: var(--diagram-flow-background) !important;
	}

	:global(.network-diagram-flow .svelte-flow__node.arcane-topology-node) {
		cursor: pointer;
		line-height: 1.45;
		transition:
			transform 180ms ease,
			box-shadow 180ms ease,
			border-color 180ms ease;
	}

	:global(.network-diagram-flow .svelte-flow__node.arcane-topology-node:hover) {
		transform: translateY(-2px);
		box-shadow: var(--diagram-node-hover-shadow);
	}

	:global(.network-diagram-flow .svelte-flow__node.arcane-topology-node.selected) {
		box-shadow: var(--diagram-node-selected-shadow);
	}

	:global(.network-diagram-flow .svelte-flow__controls) {
		border-radius: 18px;
		overflow: hidden;
		border: 1px solid var(--diagram-controls-border);
		box-shadow: var(--diagram-controls-shadow);
	}

	:global(.network-diagram-flow .svelte-flow__controls-button) {
		background: var(--diagram-controls-background);
		color: var(--diagram-controls-foreground);
	}

	:global(.network-diagram-flow .svelte-flow__minimap) {
		border-radius: 18px;
		overflow: hidden;
		border: 1px solid var(--diagram-minimap-border);
		box-shadow: var(--diagram-minimap-shadow);
	}
</style>
