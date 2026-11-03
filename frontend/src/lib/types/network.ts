export interface NetworkSummary {
	id: string;
	name: string;
	driver: string;
	scope: string;
	created: string;
	options: Record<string, string>;
	labels: Record<string, string>;
	ipam: NetworkIPAM;
	inUse: boolean;
	isDefault: boolean;
}

export interface NetworkContainerEndpoint {
	id: string;
	name: string;
	endpointId: string;
	ipv4Address: string;
	ipv6Address: string;
	macAddress: string;
}

export interface NetworkUsageCounts {
	inuse: number;
	unused: number;
	total: number;
}

export interface ConfigReference {
	Network?: string;
}

export interface PeerInfo {
	Name?: string;
	IP?: string;
}

export interface Task {
	Name?: string;
	EndpointID?: string;
	EndpointIP?: string;
	Info?: Record<string, string>;
}

export interface ServiceInfo {
	VIP?: string;
	Ports?: string[];
	LocalLBIndex?: number;
	Tasks?: Task[];
}

export interface NetworkInspect {
	id: string;
	name: string;
	driver: string;
	scope: string;
	created: string;
	ipam: NetworkIPAM;
	configFrom: ConfigReference;
	options: Record<string, string>;
	labels: Record<string, string>;
	peers?: PeerInfo[];
	services?: Record<string, ServiceInfo>;
	containersList: NetworkContainerEndpoint[];
	enableIPv4: boolean;
	enableIPv6: boolean;
	internal: boolean;
	attachable: boolean;
	ingress: boolean;
	configOnly: boolean;
}

export interface NetworkCreateResponse {
	activityId?: string;
	id: string;
	warning?: string;
}

export interface Network {
	name: string;
	options: NetworkCreateOptions;
}

export interface NetworkIPAMConfig {
	auxAddress?: Record<string, string>;
	subnet?: string;
	gateway?: string;
	ipRange?: string;
}

export interface NetworkIPAM {
	driver?: string;
	options?: Record<string, string>;
	config?: NetworkIPAMConfig[];
}

export interface NetworkCreateOptions {
	ipam?: NetworkIPAM;
	options?: Record<string, string>;
	labels?: Record<string, string>;
	driver?: string;
	checkDuplicate?: boolean;
	internal?: boolean;
	attachable?: boolean;
	ingress?: boolean;
	enableIPv6?: boolean;
}

export type NetworkTopologyNodeType = 'network' | 'container';

export interface NetworkTopologyNodeMetadata {
	driver?: string;
	scope?: string;
	status?: string;
	image?: string;
	isDefault?: boolean;
}

export interface NetworkTopologyNode {
	environmentId?: string;
	environmentName?: string;
	id: string;
	name: string;
	type: NetworkTopologyNodeType;
	metadata: NetworkTopologyNodeMetadata;
}

export interface NetworkTopologyEdge {
	id: string;
	source: string;
	target: string;
	ipv4Address?: string;
	ipv6Address?: string;
}

export interface NetworkTopology {
	nodes: NetworkTopologyNode[];
	edges: NetworkTopologyEdge[];
}
