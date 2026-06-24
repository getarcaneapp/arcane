const fs = require('fs');
const path = require('path');

const FRONTEND_DIR = path.join(__dirname, '../frontend/src');
const WHITELIST_FILE = path.join(__dirname, 'i18n-whitelist.txt');
const BLACKLIST_FILE = path.join(__dirname, 'i18n-blacklist.txt');

function loadList(filePath) {
    try {
        return fs.readFileSync(filePath, 'utf8')
            .split('\n')
            .map(l => l.trim())
            .filter(Boolean);
    } catch (e) {
        return [];
    }
}

const whitelist = loadList(WHITELIST_FILE);
const blacklist = loadList(BLACKLIST_FILE);

function walkDir(dir, callback) {
    fs.readdirSync(dir).forEach(f => {
        let dirPath = path.join(dir, f);
        let isDirectory = fs.statSync(dirPath).isDirectory();
        isDirectory ? walkDir(dirPath, callback) : callback(path.join(dir, f));
    });
}

const errors = [];

// A heuristic regex to find plain text inside tags that isn't wrapped in {m.key()} or {}
// Matches a text node between > and < with at least 3 characters
const textRegex = />\s*([^<{][^<{]{2,})\s*</g;

walkDir(FRONTEND_DIR, (filePath) => {
    if (filePath.endsWith('.svelte')) {
        const content = fs.readFileSync(filePath, 'utf8');
        
        let match;
        const lines = content.split('\n');
        
        for (let i = 0; i < lines.length; i++) {
            const line = lines[i];
            
            // Skip lines that look like purely structural markup or CSS class lists
            if (line.includes('class=') && line.includes('text-') && !line.includes('>')) continue;
            
            // Find text nodes between HTML tags
            let result;
            while ((result = textRegex.exec(line)) !== null) {
                const text = result[1].trim();
                if (
                    text.length < 3 || 
                    !isNaN(text) || 
                    ['true', 'false', 'null', 'undefined', 'promise', 'tcp', 'udp', 'bind', 'volume'].includes(text.toLowerCase()) ||
                    text.includes('===') || 
                    text.includes('=>')
                ) {
                    continue;
                }

                // Skip if the exact phrase is whitelisted
                if (whitelist.includes(text)) continue;

                // Skip if the line contains a template expression or a call to m.
                if (text.includes('{') || text.includes('m.')) continue;

                // Extra check: if it looks like an English phrase (has spaces or uppercase start)
                if (text.includes(' ') || /^[A-Z]/.test(text)) {
                    // Also skip very small words that are likely UI tokens (More, Next, Prev)
                    if (text.length <= 6 && whitelist.includes(text)) continue;
                    errors.push(`[${filePath.replace(process.cwd(), '')}:${i + 1}] Found potential hardcoded text: "${text}"`);
                }
            }

            // Also check for aria-labels or title attributes which often have hardcoded text
            const attrRegex = /(?:aria-label|title|placeholder)=["']([^"']{2,})["']/g;
            while ((result = attrRegex.exec(line)) !== null) {
                const text = result[1];
                // If attribute value contains Svelte expression or calls to m.*, skip
                if (text.includes('{') || text.includes('m.')) continue;
                // Skip whitelisted attributes (common tokens)
                if (whitelist.includes(text)) continue;
                // If blacklisted, always report
                if (blacklist.includes(text)) {
                    errors.push(`[${filePath.replace(process.cwd(), '')}:${i + 1}] Blacklisted attribute text: "${text}"`);
                    continue;
                }

                // Short tokens like 'breadcrumb' or 'More' are often structural; ignore unless uppercase start
                if (text.length <= 4 && !/^[A-Z]/.test(text)) continue;

                errors.push(`[${filePath.replace(process.cwd(), '')}:${i + 1}] Found potential hardcoded attribute text: "${text}"`);
            }
        }
    }
});

if (errors.length > 0) {
    console.log("Found potentially hardcoded text in Svelte files. Please use Paraglide i18n (m.key_name()).");
    errors.forEach(e => console.log(e));
    console.log(`\nTotal occurrences: ${errors.length}`);
    process.exit(1);
} else {
    console.log("No hardcoded text detected in Svelte files. Good job!");
    process.exit(0);
}
