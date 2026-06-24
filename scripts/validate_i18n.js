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

function analyzeContent(content, filePath) {
    const errors = [];
    
    // Strip script, style, and comments to avoid matching JS/CSS content
    // Replace with spaces but preserve newlines to keep line numbers accurate
    const preserveLength = (match) => match.replace(/[^\n]/g, ' ');
    const strippedContent = content
        .replace(/<script\b[^>]*>[\s\S]*?<\/script>/gi, preserveLength)
        .replace(/<style\b[^>]*>[\s\S]*?<\/style>/gi, preserveLength)
        .replace(/<!--[\s\S]*?-->/g, preserveLength);

    // Match any attribute value: (aria-label|title|placeholder|customLabel|etc)="value"
    // We only check common string props that might contain english text.
    // To avoid too many false positives, we check attributes ending with Label, Title, Description, or specific ones.
    const attrRegex = /(?:aria-label|title|placeholder|[a-zA-Z]+Label|[a-zA-Z]+Title|[a-zA-Z]+Description)=["']([^"']{2,})["']/g;

    // Helper to get line number (using original content for accuracy)
    const getLineNumber = (index) => content.substring(0, index).split('\n').length;

    // 1. Check for text nodes using a simple state machine
    // This avoids regex issues with > or < inside Svelte { } blocks
    let inTag = false;
    let braceDepth = 0;
    let currentText = '';
    let textStartIndex = -1;

    for (let i = 0; i < strippedContent.length; i++) {
        const char = strippedContent[i];

        if (char === '<' && braceDepth === 0) {
            inTag = true;
            if (currentText.trim().length > 0) {
                checkTextNode(currentText, textStartIndex);
            }
            currentText = '';
            textStartIndex = -1;
        } else if (char === '>' && inTag && braceDepth === 0) {
            inTag = false;
        } else if (char === '{') {
            braceDepth++;
            if (!inTag) currentText += ' '; // replace { block } with space for text check
        } else if (char === '}') {
            braceDepth = Math.max(0, braceDepth - 1);
            if (!inTag) currentText += ' ';
        } else if (!inTag && braceDepth === 0) {
            if (currentText.length === 0) textStartIndex = i;
            currentText += char;
        }
    }
    if (currentText.trim().length > 0) {
        checkTextNode(currentText, textStartIndex);
    }

    function checkTextNode(textStr, index) {
        for (let text of textStr.split('\n')) {
            text = text.trim();
            if (text.length < 3 || isWhitelisted(text)) continue;

            if (text.includes('m.')) continue;
            if (text.includes('class=') && text.includes('text-')) continue;

            if (/[a-zA-Z]/.test(text) && (text.includes(' ') || /^[A-Z]/.test(text))) {
                const lineNum = getLineNumber(index);
                errors.push(`[${filePath}:${lineNum}] Found potential hardcoded text: "${text}"`);
            }
        }
    }

    // 2. Check for attribute text
    for (const match of content.matchAll(attrRegex)) {
        const text = match[1];
        
        if (text.includes('{') || text.includes('m.')) continue;
        if (isWhitelisted(text)) continue;

        const lineNum = getLineNumber(match.index);

        if (BLACKLIST.includes(text)) {
            errors.push(`[${filePath}:${lineNum}] Blacklisted attribute text: "${text}"`);
            continue;
        }

        // Short tokens often structural; ignore unless uppercase start or multiple words
        if (text.length <= 4 && !/^[A-Z]/.test(text)) continue;
        if (!/[a-zA-Z]/.test(text)) continue;

        errors.push(`[${filePath}:${lineNum}] Found potential hardcoded attribute text: "${text}"`);
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
            const relPath = absolutePath.replace(process.cwd(), '');
            
            const fileErrors = analyzeContent(content, relPath);
            allErrors.push(...fileErrors);
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
    module.exports = { analyzeContent, isWhitelisted };
}
