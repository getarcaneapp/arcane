import { networkService } from '#lib/services/network-service.js';
import type { EnvironmentRef } from '#lib/stores/environment.store.svelte.js';
import type { NetworkTopologyEdge, NetworkTopologyNode } from '#lib/types/network.js';
import {
	environmentRows,
	readAcrossEnvironments,
	readableEnvironments,
	type EnvironmentFailure,
	type EnvironmentRow
} from '#lib/utils/environments.js';

export type EnvironmentTopology = {
	nodes: EnvironmentRow<NetworkTopologyNode>[];
	edges: NetworkTopologyEdge[];
	failedEnvironments: EnvironmentFailure[];
	excludedEnvironments: EnvironmentRef[];
};

export async function fetchEnvironmentTopology(): Promise<EnvironmentTopology> {
	const environments = readableEnvironments('networks:read');
	if (environments.length === 0) return { nodes: [], edges: [], failedEnvironments: [], excludedEnvironments: [] };
	const { value, failures, excluded } = await readAcrossEnvironments(environments, (scope, config) =>
		networkService.getNetworkTopology(scope, config)
	);
	return {
		nodes: environmentRows(value.nodes, environments),
		edges: value.edges,
		failedEnvironments: failures,
		excludedEnvironments: excluded
	};
}
