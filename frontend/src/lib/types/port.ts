export interface PortMapping {
	id: string;
	containerId: string;
	containerName: string;
	hostIp?: string;
	protocol: string;
	hostPort?: number;
	containerPort: number;
	isPublished: boolean;
}
