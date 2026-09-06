import { m } from '#lib/paraglide/messages.js';
import type { SwarmNodeAgentState } from '#lib/types/swarm.js';

const agentStateDisplay = new Map(
	Object.entries({
		pending: { label: m.common_pending, variant: 'amber' },
		offline: { label: m.common_offline, variant: 'red' },
		connected: { label: m.swarm_node_agent_status_connected, variant: 'green' },
		mismatched: { label: m.swarm_node_agent_status_mismatched, variant: 'amber' },
		ambiguous: { label: m.swarm_node_agent_status_ambiguous, variant: 'amber' },
		none: { label: m.swarm_node_agent_status_none, variant: 'gray' }
	} satisfies Record<SwarmNodeAgentState, { label: () => string; variant: 'green' | 'red' | 'amber' | 'gray' }>)
);

export function getSwarmNodeAgentLabel(state: SwarmNodeAgentState | null | undefined): string {
	return (agentStateDisplay.get(state ?? 'none')?.label ?? m.swarm_node_agent_status_none)();
}

export function getSwarmNodeAgentVariant(state: SwarmNodeAgentState | null | undefined): 'green' | 'red' | 'amber' | 'gray' {
	return agentStateDisplay.get(state ?? 'none')?.variant ?? 'gray';
}

export function getSwarmNodeAgentActionLabel(state: SwarmNodeAgentState | null | undefined): string {
	return state === 'connected' ? m.swarm_node_agent_view() : m.swarm_node_agent_deploy();
}
