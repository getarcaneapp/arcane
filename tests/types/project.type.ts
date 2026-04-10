export type Project = {
	id?: string;
	name?: string;
	status?: string;
	serviceCount?: number;
	[key: string]: any;
};

export interface ProjectStatusCounts {
	runningProjects: number;
	stoppedProjects: number;
	totalProjects: number;
}
