export interface PortMappingDto {
	id: string;
	containerId: string;
	containerName: string;
	hostIp?: string;
	hostPort?: number;
	containerPort: number;
	protocol: string;
	isPublished: boolean;
}
