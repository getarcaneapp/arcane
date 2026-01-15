export interface SecretCreateRequest {
	name: string;
	content: string;
	description?: string;
}

export interface SecretUpdateRequest {
	name?: string;
	content?: string;
	description?: string;
}

export interface SecretSummaryDto {
	id: string;
	name: string;
	environmentId: string;
	composePath: string;
	description?: string;
	createdAt: string;
	updatedAt?: string;
}

export interface SecretWithContentDto extends SecretSummaryDto {
	content: string;
}
