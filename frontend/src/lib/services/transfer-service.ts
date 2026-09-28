import BaseAPIService from './api-service';
import { environmentStore } from '#lib/stores/environment.store.svelte.js';
import type {
	Transfer,
	TransferCleanupRequest,
	TransferCleanupResponse,
	TransferCreateRequest,
	TransferKind,
	TransferPlan,
	TransferRequest
} from '#lib/types/transfer.type.js';

// Every route is scoped to the source environment (always the selected one)
// and to the resource kind, which carries its own transfer permission.
class TransferService extends BaseAPIService {
	private async basePath(kind: TransferKind): Promise<string> {
		const envId = await environmentStore.getCurrentEnvironmentId();
		const segment = kind === 'project' ? 'projects' : 'volumes';
		return `/environments/${envId}/transfers/${segment}`;
	}

	private async transferPath(kind: TransferKind, transferId: string): Promise<string> {
		return `${await this.basePath(kind)}/${encodeURIComponent(transferId)}`;
	}

	async preflight(request: TransferRequest): Promise<TransferPlan> {
		return this.handleResponse(this.api.post(`${await this.basePath(request.kind)}/preflight`, request));
	}

	async create(request: TransferCreateRequest): Promise<Transfer> {
		return this.handleResponse(this.api.post(await this.basePath(request.request.kind), request));
	}

	async list(kind: TransferKind): Promise<Transfer[]> {
		return this.handleResponse(this.api.get(await this.basePath(kind)));
	}

	async get(kind: TransferKind, transferId: string): Promise<Transfer> {
		return this.handleResponse(this.api.get(await this.transferPath(kind, transferId)));
	}

	async cancel(kind: TransferKind, transferId: string): Promise<Transfer> {
		return this.handleResponse(this.api.post(`${await this.transferPath(kind, transferId)}/cancel`));
	}

	async retry(kind: TransferKind, transferId: string): Promise<Transfer> {
		return this.handleResponse(this.api.post(`${await this.transferPath(kind, transferId)}/retry`));
	}

	async rollback(kind: TransferKind, transferId: string): Promise<Transfer> {
		return this.handleResponse(
			this.api.post(`${await this.transferPath(kind, transferId)}/rollback`, {
				acknowledgeDestinationWritesDiscarded: true
			})
		);
	}

	async cleanup(kind: TransferKind, transferId: string, request: TransferCleanupRequest): Promise<TransferCleanupResponse> {
		return this.handleResponse(this.api.post(`${await this.transferPath(kind, transferId)}/cleanup`, request));
	}

	async releaseHold(kind: TransferKind, transferId: string): Promise<Transfer> {
		return this.handleResponse(this.api.post(`${await this.transferPath(kind, transferId)}/release-hold`));
	}
}

export const transferService = new TransferService();
