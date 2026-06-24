const fs = require('fs');
const path = require('path');

const FRONTEND_DIR = path.join(__dirname, '../frontend/src');

// ============================================================================
// CONFIGURATION
// ============================================================================

// Technical words or patterns that are NOT text to translate.
const WHITELIST = [
    'compose.yaml', '.env', '#3b82f6', '80', '8080', '1000:1000', '30', '60', 
    '300', '600', '900', '0 */15 * * * *', '/bin/sh', '-c echo hello', '/app', 
    '/target', 'main', 'my-service', 'KEY', 'value', 'Key', 'Value', 'key', 
    'source', 'ghcr.io/getarcaneapp/tools:latest', 'https://github.com/user/repo.git',
    'true', 'false', 'null', 'undefined', 'promise', 'tcp', 'udp', 'bind', 'volume',
    'breadcrumb', 'data.projects as paginated'
];

// Very common UI words that MUST ALWAYS be translated, 
// even if they are very short and could slip through the cracks.
const BLACKLIST = [
    'Close', 'Cancel', 'Save', 'More', 'Copy', 'Copied', 'Edit', 'Delete'
];

// ============================================================================
// CORE LOGIC (Exported for testing)
// ============================================================================

// Matches a text node between > and < with at least 3 characters.
// We avoid Svelte control blocks like {#each} or {if} by ensuring the text 
// contains alphanumeric characters and standard punctuation, without starting with # or / or {
const textRegex = />\s*([A-Za-z0-9][A-Za-z0-9\s.,'?!-]{2,})\s*</g;
const attrRegex = /(?:aria-label|title|placeholder)=["']([^"']{2,})["']/g;

function isWhitelisted(text) {
    const lower = text.toLowerCase();
    return (
        !isNaN(text) || // Numbers are ignored
        WHITELIST.includes(text) ||
        WHITELIST.map(w => w.toLowerCase()).includes(lower) ||
        text.includes('===') || 
        text.includes('=>')
    );
}

function analyzeLine(line, filePath, lineNumber) {
    const errors = [];
    
    // Skip purely structural markup or tailwind class lines
    if (line.includes('class=') && line.includes('text-') && !line.includes('>')) return errors;

    // 1. Check for text nodes
    let result;
    while ((result = textRegex.exec(line)) !== null) {
        const text = result[1].trim();
        
        // Skip too short, numbers, or whitelisted technical terms
        if (text.length < 3 || isWhitelisted(text)) continue;

        // Skip if it contains a svelte expression { } or Paraglide m.*
        if (text.includes('{') || text.includes('m.')) continue;

        // Check if it looks like an English phrase (spaces or starts with uppercase)
        if (text.includes(' ') || /^[A-Z]/.test(text)) {
            errors.push(`[${filePath}:${lineNumber}] Found potential hardcoded text: "${text}"`);
        }
    }

    // 2. Check for common attributes (aria-label, title, placeholder)
    while ((result = attrRegex.exec(line)) !== null) {
        const text = result[1];
        
        if (text.includes('{') || text.includes('m.')) continue;
        if (isWhitelisted(text)) continue;

        if (BLACKLIST.includes(text)) {
            errors.push(`[${filePath}:${lineNumber}] Blacklisted attribute text: "${text}"`);
            continue;
        }

        // Short tokens often structural; ignore unless uppercase start or multiple words
        if (text.length <= 4 && !/^[A-Z]/.test(text)) continue;

        errors.push(`[${filePath}:${lineNumber}] Found potential hardcoded attribute text: "${text}"`);
    }

    return errors;
}

// ============================================================================
// CLI EXECUTION
// ============================================================================

function walkDir(dir, callback) {
    fs.readdirSync(dir).forEach(f => {
        let dirPath = path.join(dir, f);
        let isDirectory = fs.statSync(dirPath).isDirectory();
        isDirectory ? walkDir(dirPath, callback) : callback(path.join(dir, f));
    });
}

function run() {
    const allErrors = [];
    walkDir(FRONTEND_DIR, (absolutePath) => {
        if (absolutePath.endsWith('.svelte')) {
            const content = fs.readFileSync(absolutePath, 'utf8');
            const lines = content.split('\n');
            const relPath = absolutePath.replace(process.cwd(), '');
            
            for (let i = 0; i < lines.length; i++) {
                const lineErrors = analyzeLine(lines[i], relPath, i + 1);
                allErrors.push(...lineErrors);
            }
        }
    });

    if (allErrors.length > 0) {
        console.log("Found potentially hardcoded text in Svelte files. Please use Paraglide i18n (m.key_name()).");
        allErrors.forEach(e => console.log(e));
        console.log(`\nTotal occurrences: ${allErrors.length}`);
        process.exit(1);
    } else {
        console.log("No hardcoded text detected in Svelte files. Good job!");
        process.exit(0);
    }
}

// Export for testing, or run if main
if (require.main === module) {
    run();
} else {
    module.exports = { analyzeLine, isWhitelisted };
}
