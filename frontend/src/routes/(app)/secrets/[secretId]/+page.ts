import type { PageLoad } from './$types';
import { error } from '@sveltejs/kit';
import { secretService } from '$lib/services/secret-service';

export const load: PageLoad = async ({ params }) => {
	const { secretId } = params;

	try {
		const secret = await secretService.getSecret(secretId);
		const secretWithContent = await secretService.getSecretContent(secretId);

		return {
			secret,
			content: secretWithContent.content
		};
	} catch (err: any) {
		console.error('Failed to load secret:', err);
		if (err.status === 404) throw err;
		throw error(500, err.message || 'Failed to load secret details');
	}
};
