import { m } from '$lib/paraglide/messages';
import type { SwarmNodeAgentState } from '$lib/types/swarm.type';

export function getSwarmNodeAgentLabel(state: SwarmNodeAgentState | null | undefined): string {
	switch (state) {
		case 'pending':
			return m.swarm_node_agent_status_pending();
		case 'offline':
			return m.swarm_node_agent_status_offline();
		case 'connected':
			return m.swarm_node_agent_status_connected();
		case 'mismatched':
			return m.swarm_node_agent_status_mismatched();
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
			return 'amber';
		case 'none':
		default:
			return 'gray';
	}
}

export function getSwarmNodeAgentActionLabel(state: SwarmNodeAgentState | null | undefined): string {
	return state === 'connected' ? m.swarm_node_agent_view() : m.swarm_node_agent_deploy();
}
