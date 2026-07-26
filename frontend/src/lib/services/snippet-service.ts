import BaseAPIService from './api-service';
import type { Snippet, SnippetRun, SnippetCreateDto, SnippetUpdateDto, RunSnippetDto } from '#lib/types/snippet';
import type { Paginated, SearchPaginationSortRequest } from '#lib/types/shared';
import { transformPaginationParams } from '#lib/utils/tables';

class SnippetService extends BaseAPIService {
	async getSnippets(environmentId: string, options?: SearchPaginationSortRequest): Promise<Paginated<Snippet>> {
		const params = transformPaginationParams(options);
		const res = await this.api.get(`/environments/${environmentId}/snippets`, { params });
		return res.data;
	}

	async getSnippet(environmentId: string, snippetId: string): Promise<Snippet> {
		return this.handleResponse(this.api.get(`/environments/${environmentId}/snippets/${snippetId}`));
	}

	async createSnippet(environmentId: string, snippet: SnippetCreateDto): Promise<Snippet> {
		return this.handleResponse(this.api.post(`/environments/${environmentId}/snippets`, snippet));
	}

	async updateSnippet(environmentId: string, snippetId: string, snippet: SnippetUpdateDto): Promise<Snippet> {
		return this.handleResponse(this.api.put(`/environments/${environmentId}/snippets/${snippetId}`, snippet));
	}

	async deleteSnippet(environmentId: string, snippetId: string): Promise<void> {
		return this.handleResponse(this.api.delete(`/environments/${environmentId}/snippets/${snippetId}`));
	}

	async runSnippet(environmentId: string, snippetId: string, request: RunSnippetDto): Promise<SnippetRun> {
		return this.handleResponse(this.api.post(`/environments/${environmentId}/snippets/${snippetId}/run`, request));
	}

	async getSnippetRuns(
		environmentId: string,
		snippetId: string,
		options?: SearchPaginationSortRequest
	): Promise<Paginated<SnippetRun>> {
		const params = transformPaginationParams(options);
		const res = await this.api.get(`/environments/${environmentId}/snippets/${snippetId}/runs`, { params });
		return res.data;
	}
}

export const snippetService = new SnippetService();
