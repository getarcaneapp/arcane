const LOCAL_PART_PATTERN = /^[A-Za-z0-9!#$%&'*+/=?^_`{|}~.-]+$/;
const DOMAIN_LABEL_PATTERN = /^[\p{L}\p{N}](?:[\p{L}\p{N}-]{0,61}[\p{L}\p{N}])?$/u;

export function isValidUserEmail(email: string): boolean {
	const trimmedEmail = email.trim();
	if (!trimmedEmail || trimmedEmail.includes(' ')) {
		return false;
	}

	const atIndex = trimmedEmail.indexOf('@');
	if (atIndex <= 0 || atIndex !== trimmedEmail.lastIndexOf('@') || atIndex === trimmedEmail.length - 1) {
		return false;
	}

	const localPart = trimmedEmail.slice(0, atIndex);
	const domainPart = trimmedEmail.slice(atIndex + 1);

	return isValidLocalPart(localPart) && isValidDomainPart(domainPart);
}

function isValidLocalPart(localPart: string): boolean {
	if (!localPart || localPart.length > 64 || localPart.startsWith('.') || localPart.endsWith('.') || localPart.includes('..')) {
		return false;
	}

	return LOCAL_PART_PATTERN.test(localPart);
}

function isValidDomainPart(domainPart: string): boolean {
	if (!domainPart || domainPart.length > 255) {
		return false;
	}

	if (isValidIPv4Literal(domainPart)) {
		return true;
	}

	if (isValidIPv6Literal(domainPart)) {
		return true;
	}

	const labels = domainPart.split('.');
	if (labels.length === 4 && labels.every((label) => /^\d+$/.test(label))) {
		return false;
	}

	if (labels.some((label) => !DOMAIN_LABEL_PATTERN.test(label))) {
		return false;
	}

	return true;
}

function isValidIPv4Literal(domainPart: string): boolean {
	const octets = domainPart.split('.');
	if (octets.length !== 4) {
		return false;
	}

	return octets.every((octet) => /^\d+$/.test(octet) && Number(octet) >= 0 && Number(octet) <= 255);
}

function isValidIPv6Literal(domainPart: string): boolean {
	if (!/^\[IPv6:[0-9A-Fa-f:.]+\]$/i.test(domainPart)) {
		return false;
	}

	const address = domainPart.slice(6, -1);
	return isValidIPv6Address(address);
}

function isValidIPv6Address(address: string): boolean {
	if (!address.includes(':') || address.includes(':::')) {
		return false;
	}

	const compressionIndex = address.indexOf('::');
	if (compressionIndex !== -1 && compressionIndex !== address.lastIndexOf('::')) {
		return false;
	}

	if (compressionIndex === -1) {
		return countIPv6Segments(address.split(':')) === 8;
	}

	const [left = '', right = ''] = address.split('::');
	const leftCount = left ? countIPv6Segments(left.split(':')) : 0;
	const rightCount = right ? countIPv6Segments(right.split(':')) : 0;

	return leftCount >= 0 && rightCount >= 0 && leftCount + rightCount < 8;
}

function countIPv6Segments(segments: string[]): number {
	let count = 0;

	for (let i = 0; i < segments.length; i += 1) {
		const segment = segments[i];
		if (!segment) {
			return -1;
		}

		const isLastSegment = i === segments.length - 1;
		if (segment.includes('.')) {
			return isLastSegment && isValidIPv4Literal(segment) ? count + 2 : -1;
		}

		if (!/^[0-9A-Fa-f]{1,4}$/.test(segment)) {
			return -1;
		}

		count += 1;
	}

	return count;
}
