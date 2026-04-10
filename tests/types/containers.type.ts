export type ContainerSummary = {
	id: string;
	names?: string[];
	image?: string;
	state: string;
	status?: string;
	created?: number;
};
