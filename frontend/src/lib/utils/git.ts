function stripGitSuffix(path: string): string {
	return path.replace(/\.git\/?$/, '');
}

function trimTrailingSlash(value: string): string {
	return value.replace(/\/+$/, '');
}

function commitSegmentForHost(hostname: string): string {
	const host = hostname.toLowerCase();
	if (host.includes('gitlab')) return '/-/commit/';
	if (host.includes('bitbucket')) return '/commits/';
	return '/commit/';
}

export function toGitWebUrl(raw: string): string | null {
	const trimmed = raw.trim();
	if (!trimmed) return null;

	if (trimmed.includes('://')) {
		try {
			const parsed = new URL(trimmed);
			if (!parsed.hostname) return null;
			const path = stripGitSuffix(parsed.pathname);
			if (!path || path === '/') return null;
			const protocol = parsed.protocol === 'http:' || parsed.protocol === 'https:' ? parsed.protocol : 'https:';
			return `${protocol}//${parsed.hostname}${path}`;
		} catch {
			return null;
		}
	}

	const scpMatch = /^(?:.+@)?([^:\/]+):(.+)$/.exec(trimmed);
	if (scpMatch) {
		const host = scpMatch[1];
		const path = stripGitSuffix(scpMatch[2].replace(/^\/+/, ''));
		if (!host || !path) return null;
		return `https://${host}/${path}`;
	}

	const hostPathMatch = /^([^\/]+)\/(.+)$/.exec(trimmed);
	if (hostPathMatch) {
		const host = hostPathMatch[1];
		const path = stripGitSuffix(hostPathMatch[2].replace(/^\/+/, ''));
		if (!host || !path) return null;
		return `https://${host}/${path}`;
	}

	return null;
}

export function toGitCommitUrl(repositoryUrl: string, commit: string): string | null {
	const base = toGitWebUrl(repositoryUrl);
	const trimmedCommit = commit.trim();
	if (!base || !trimmedCommit) return null;

	const normalizedBase = trimTrailingSlash(base);
	try {
		const host = new URL(normalizedBase).hostname;
		const segment = commitSegmentForHost(host);
		return `${normalizedBase}${segment}${encodeURIComponent(trimmedCommit)}`;
	} catch {
		return null;
	}
}
