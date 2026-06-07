import crypto from 'node:crypto';
import http from 'node:http';

type JwtClaims = Record<string, unknown>;

export type MockOidcIssuer = {
	issuerURL: string;
	token: (claims: JwtClaims) => string;
	close: () => Promise<void>;
};

function base64url(value: Buffer | string): string {
	return Buffer.from(value).toString('base64url');
}

function signJWT(privateKey: crypto.KeyObject, keyID: string, claims: JwtClaims): string {
	const header = base64url(JSON.stringify({ alg: 'RS256', kid: keyID, typ: 'JWT' }));
	const payload = base64url(JSON.stringify(claims));
	const input = `${header}.${payload}`;
	const signature = crypto.createSign('RSA-SHA256').update(input).sign(privateKey, 'base64url');

	return `${input}.${signature}`;
}

export async function createMockOidcIssuer(): Promise<MockOidcIssuer> {
	const keyID = `arcane-e2e-${crypto.randomUUID()}`;
	const { privateKey, publicKey } = crypto.generateKeyPairSync('rsa', {
		modulusLength: 2048
	});
	const publicJWK = publicKey.export({ format: 'jwk' });

	let issuerURL = '';
	const server = http.createServer((req, res) => {
		const path = req.url?.split('?')[0];

		if (path === '/.well-known/openid-configuration') {
			res.writeHead(200, { 'Content-Type': 'application/json' });
			res.end(
				JSON.stringify({
					issuer: issuerURL,
					jwks_uri: `${issuerURL}/jwks`,
					authorization_endpoint: `${issuerURL}/authorize`,
					token_endpoint: `${issuerURL}/token`
				})
			);
			return;
		}

		if (path === '/jwks') {
			res.writeHead(200, { 'Content-Type': 'application/json' });
			res.end(
				JSON.stringify({
					keys: [{ ...publicJWK, kid: keyID, use: 'sig', alg: 'RS256' }]
				})
			);
			return;
		}

		res.writeHead(404);
		res.end();
	});

	await new Promise<void>((resolve, reject) => {
		server.once('error', reject);
		server.listen(0, '0.0.0.0', () => {
			server.off('error', reject);
			resolve();
		});
	});

	const address = server.address();
	if (!address || typeof address === 'string') {
		throw new Error('mock OIDC issuer did not bind to a TCP port');
	}

	issuerURL = `http://host.docker.internal:${address.port}`;

	return {
		issuerURL,
		token: (claims) => signJWT(privateKey, keyID, claims),
		close: () =>
			new Promise<void>((resolve, reject) => {
				server.close((error) => (error ? reject(error) : resolve()));
			})
	};
}
