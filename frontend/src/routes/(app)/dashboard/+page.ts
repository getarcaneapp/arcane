import type { PageLoad } from './$types';

export const load: PageLoad = async ({ url }) => {
	const debugAllGood = url.searchParams.get('debugAllGood') === 'true';

	return {
		debugAllGood
	};
};
