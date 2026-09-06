export function isOpenQuote(value: string): string | null {
	// Simplification: this only detects obviously unterminated leading quotes.
	// Values that start/end with the same quote are treated as closed, even if
	// their internal quoting would be malformed in a stricter parser.
	if (value.length === 0) return null;
	const quote = value[0];
	if (quote !== '"' && quote !== "'") return null;
	if (value.length >= 2 && value[value.length - 1] === quote) return null;
	return quote;
}

export function hasClosingQuote(line: string, quote: string): boolean {
	const end = line.trimEnd();
	return end.endsWith(quote) && !(end.length >= 2 && end[end.length - 2] === '\\');
}
