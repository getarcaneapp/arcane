import { snippetService } from '#lib/services/snippet-service';
import { queryKeys } from '#lib/query/query-keys';
import { resolveListPageLoadContext } from '#lib/utils/tables';
import { throwPageLoadError } from '#lib/utils/api';
import type { PageLoad } from './$types';

export const load: PageLoad = async ({ parent }) => {
	const {
		queryClient,
		envId,
		requestOptions: snippetRequestOptions
	} = await resolveListPageLoadContext(parent, 'arcane-snippets-table', {
		column: 'name',
		direction: 'asc'
	});

	let snippets;
	try {
		snippets = await queryClient.fetchQuery({
			queryKey: queryKeys.snippets.list(envId, snippetRequestOptions),
			queryFn: () => snippetService.getSnippets(envId, snippetRequestOptions)
		});
	} catch (err) {
		throwPageLoadError(err, 'Failed to load snippets');
	}

	return { envId, snippets, snippetRequestOptions };
};
