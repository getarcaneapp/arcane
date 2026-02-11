import { redirect } from '@sveltejs/kit';
import type { PageLoad } from './$types';
import { authService } from '$lib/services/auth-service';
import { queryKeys } from '$lib/query/query-keys';

export const load: PageLoad = async ({ fetch, parent }) => {
	const { queryClient } = await parent();

	try {
		await fetch('/api/auth/logout', {
			method: 'POST',
			credentials: 'include'
		});
	} catch (error) {
		console.error('Logout error:', error);
	}

	authService.logout();
	queryClient.removeQueries({ queryKey: queryKeys.auth.all });

	throw redirect(302, '/login');
};
