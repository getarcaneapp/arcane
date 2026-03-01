export interface TemplateRegistry {
	id: string;
	name: string;
	url: string;
	enabled: boolean;
	description: string;
	createdAt?: string;
	updatedAt?: string;
}

export interface Template {
	id: string;
	name: string;
	description: string;
	content: string;
	envContent?: string;
	isCustom: boolean;
	isRemote: boolean;
	registryId?: string;
	registry?: TemplateRegistry;
	metadata?: {
		version?: string;
		author?: string;
		tags?: string[];
		remoteUrl?: string;
		envUrl?: string;
		documentationUrl?: string;
		iconUrl?: string;
		updatedAt?: string;
	};
	createdAt: string;
	updatedAt: string;
}

export interface EnvVariable {
	key: string;
	value: string;
}

export interface TemplateContentData {
	template: Template;
	content: string;
	envContent: string;
	services: string[];
	envVariables: EnvVariable[];
}

export interface RemoteTemplate {
	id: string;
	name: string;
	description: string;
	version: string;
	author: string;
	tags: string[];
	compose_url: string;
	env_url?: string;
	documentation_url?: string;
}

export interface RemoteRegistry {
	$schema?: string;
	name: string;
	description: string;
	version: string;
	author: string;
	url: string;
	templates: RemoteTemplate[];
}

export interface TemplateRegistryConfig {
	url: string;
	name: string;
	enabled: boolean;
}
