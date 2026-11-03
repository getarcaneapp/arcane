import type { UploadProgressCallback } from '#lib/types/ui/upload.js';
import type { UploadKind, UploadSession } from '#lib/types/upload.js';
import { tryCatch } from '#lib/utils/try-catch.js';

import BaseAPIService from './api-service';

const chunkRetryAttempts = 3;
const chunkRetryDelayMs = 1000;

class UploadService extends BaseAPIService {
	async createSession(envId: string, kind: UploadKind, file: File): Promise<UploadSession> {
		return this.handleResponse(this.api.post(`/environments/${envId}/uploads/${kind}`, { filename: file.name, size: file.size }));
	}

	async putChunk(envId: string, kind: UploadKind, uploadId: string, index: number, chunk: Blob): Promise<UploadSession> {
		return this.handleResponse(
			this.api.put(`/environments/${envId}/uploads/${kind}/${uploadId}/chunks/${index}`, chunk, {
				headers: { 'Content-Type': 'application/octet-stream' }
			})
		);
	}

	async getSession(envId: string, kind: UploadKind, uploadId: string): Promise<UploadSession> {
		return this.handleResponse(this.api.get(`/environments/${envId}/uploads/${kind}/${uploadId}`));
	}

	async deleteSession(envId: string, kind: UploadKind, uploadId: string): Promise<void> {
		await this.api.delete(`/environments/${envId}/uploads/${kind}/${uploadId}`);
	}

	// Uploads a file as sequential chunks with per-chunk retry, so it passes
	// reverse proxies that reject large request bodies. Returns the uploadId of
	// the complete session for the caller to pass to the consuming endpoint.
	async uploadFile(envId: string, kind: UploadKind, file: File, onProgress?: UploadProgressCallback): Promise<string> {
		const session = await this.createSession(envId, kind, file);
		let received = new Set(session.receivedChunks);

		const chunkBytes = (index: number) =>
			index === session.totalChunks - 1 ? file.size - index * session.chunkSize : session.chunkSize;
		const report = () => {
			if (!onProgress) return;
			let bytesDone = 0;
			for (const index of received) bytesDone += chunkBytes(index);
			onProgress({ chunksDone: received.size, totalChunks: session.totalChunks, bytesDone, totalBytes: file.size });
		};
		report();

		for (let index = 0; index < session.totalChunks; index++) {
			if (received.has(index)) continue;
			const start = index * session.chunkSize;
			const chunk = file.slice(start, Math.min(start + session.chunkSize, file.size));

			for (let attempt = 1; ; attempt++) {
				const chunkResult = await tryCatch(
					(async () => {
						const updated = await this.putChunk(envId, kind, session.id, index, chunk);
						received = new Set(updated.receivedChunks);
					})()
				);
				if (chunkResult.error === null) break;
				if (attempt >= chunkRetryAttempts) {
					await tryCatch(this.deleteSession(envId, kind, session.id));
					throw chunkResult.error;
				}
				await new Promise((resolve) => setTimeout(resolve, chunkRetryDelayMs * attempt));
				// The status check is best-effort; resume anything the server already has.
				const statusResult = await tryCatch(
					(async () => {
						received = new Set((await this.getSession(envId, kind, session.id)).receivedChunks);
						return received.has(index);
					})()
				);
				if (statusResult.data) break;
			}
			report();
		}
		return session.id;
	}
}

export const uploadService = new UploadService();
