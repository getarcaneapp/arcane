export type SnippetParameterType = 'string' | 'number' | 'boolean' | 'select';
export type SnippetTarget = 'host' | 'container';

export interface SnippetParameterDef {
	name: string;
	type: SnippetParameterType;
	label?: string;
	required?: boolean;
	default?: string;
	options?: string[];
	pattern?: string;
}

export interface Snippet {
	id: string;
	createdAt: string;
	updatedAt: string;
	environmentId: string;
	name: string;
	description?: string;
	script: string;
	target: SnippetTarget;
	containerId?: string;
	parameters?: SnippetParameterDef[];
	workingDir?: string;
	timeoutSec: number;
	schedule?: string;
	scheduleEnabled: boolean;
	scheduleParameters?: Record<string, string>;
	lastRunAt?: string;
	lastRunStatus?: string;
	createdByUserId?: string;
}

export interface SnippetRun {
	id: string;
	createdAt: string;
	updatedAt: string;
	snippetId: string;
	environmentId: string;
	triggerSource: string;
	status: string;
	exitCode?: number;
	parameters?: Record<string, string>;
	output?: string;
	error?: string;
	startedAt: string;
	durationMs: number;
	startedByUserId?: string;
	startedByUsername?: string;
}

export interface SnippetCreateDto {
	name: string;
	description?: string;
	script: string;
	target?: SnippetTarget;
	containerId?: string;
	parameters?: SnippetParameterDef[];
	workingDir?: string;
	timeoutSec?: number;
	schedule?: string;
	scheduleEnabled?: boolean;
	scheduleParameters?: Record<string, string>;
}

export interface SnippetUpdateDto {
	name?: string;
	description?: string;
	script?: string;
	target?: SnippetTarget;
	containerId?: string;
	parameters?: SnippetParameterDef[];
	workingDir?: string;
	timeoutSec?: number;
	schedule?: string;
	scheduleEnabled?: boolean;
	scheduleParameters?: Record<string, string>;
}

export interface RunSnippetDto {
	parameters?: Record<string, string>;
}
