import { m } from '$lib/paraglide/messages';
import type { SwarmNodeAgentState } from '$lib/types/swarm';

export function getSwarmNodeAgentLabel(state: SwarmNodeAgentState | null | undefined): string {
	switch (state) {
		case 'pending':
			return m.common_pending();
		case 'offline':
			return m.common_offline();
		case 'connected':
			return m.swarm_node_agent_status_connected();
		case 'mismatched':
			return m.swarm_node_agent_status_mismatched();
		case 'ambiguous':
			return m.swarm_node_agent_status_ambiguous();
		case 'none':
		default:
			return m.swarm_node_agent_status_none();
	}
}

export function getSwarmNodeAgentVariant(state: SwarmNodeAgentState | null | undefined): 'green' | 'red' | 'amber' | 'gray' {
	switch (state) {
		case 'connected':
			return 'green';
		case 'offline':
			return 'red';
		case 'pending':
		case 'mismatched':
		case 'ambiguous':
			return 'amber';
		case 'none':
		default:
			return 'gray';
	}
}

export function getSwarmNodeAgentActionLabel(state: SwarmNodeAgentState | null | undefined): string {
	return state === 'connected' ? m.swarm_node_agent_view() : m.swarm_node_agent_deploy();
}
