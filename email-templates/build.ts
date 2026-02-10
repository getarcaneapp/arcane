import { render } from '@react-email/components';
import * as fs from 'node:fs';
import * as path from 'node:path';

const outputDir = '../backend/resources/email-templates';

if (!fs.existsSync(outputDir)) {
  fs.mkdirSync(outputDir, { recursive: true });
}

function getTemplateName(filename: string): string {
  return filename.replace('.tsx', '');
}

/**
 * Tag-aware wrapping:
 * - Prefer breaking immediately after the last '>' within maxLen.
 * - Never break at spaces.
 * - If no '>' exists in the window, hard-break at maxLen.
 */
function tagAwareWrap(input: string, maxLen: number): string {
  const out: string[] = [];

  for (const originalLine of input.split(/\r?\n/)) {
    let line = originalLine;
    while (line.length > maxLen) {
      let breakPos = line.lastIndexOf('>', maxLen);

      // If '>' happens to be exactly at maxLen, break after it
      if (breakPos === maxLen) breakPos = maxLen;

      // If we found a '>' before the limit, break right after it
      if (breakPos > -1 && breakPos < maxLen) {
        out.push(line.slice(0, breakPos + 1));
        line = line.slice(breakPos + 1);
        continue;
      }

      // No suitable tag end found—hard break
      out.push(line.slice(0, maxLen));
      line = line.slice(maxLen);
    }
    out.push(line);
  }

  return out.join('\n');
}

async function buildTemplateFile(Component: any, templateName: string, isPlainText: boolean) {
  const rendered = await render(Component(Component.TemplateProps), {
    plainText: isPlainText,
  });

  // Normalize quotes
  let normalized = rendered.replace(/&quot;/g, '"');

  // Post-process: Replace special placeholders with Go template syntax
  // This allows us to inject Go template loops that can't be rendered by React
  if (isPlainText) {
    // For plain text, use simple line-by-line rendering
    normalized = normalized.replace(
      /IMAGELIST_PLACEHOLDER/g,
      '{{range .ImageList}}• {{.}}\n{{end}}'
    );
  } else {
    // For HTML, wrap each item in a paragraph tag with proper styling
    normalized = normalized.replace(
      /<p[^>]*>IMAGELIST_PLACEHOLDER<\/p>/g,
      '{{range .ImageList}}<p style="font-size:13px;line-height:20px;color:#cbd5e1;margin:4px 0;font-family:monospace">• {{.}}</p>{{end}}'
    );
  }

  // Enforce line length: prefer tag boundaries, never spaces
  const maxLen = isPlainText ? 78 : 998; // RFC-safe
  const safe = tagAwareWrap(normalized, maxLen);

  const goTemplate = `{{define "root"}}${safe}{{end}}\n`;
  const suffix = isPlainText ? '_text.tmpl' : '_html.tmpl';
  const templatePath = path.join(outputDir, `${templateName}${suffix}`);

  fs.writeFileSync(templatePath, goTemplate);
}

async function discoverAndBuildTemplates() {
  console.log('Discovering and building email templates...');

  const emailsDir = './emails';
  const files = fs.readdirSync(emailsDir);

  for (const file of files) {
    if (!file.endsWith('.tsx')) continue;

    const templateName = getTemplateName(file);
    const modulePath = `./${emailsDir}/${file}`;

    console.log(`Building ${templateName}...`);

    try {
      const module = await import(modulePath);
      const Component = module.default || module[Object.keys(module)[0]];

      if (!Component) {
        console.error(`✗ No component found in ${file}`);
        continue;
      }

      if (!Component.TemplateProps) {
        console.error(`✗ No TemplateProps found in ${file}`);
        continue;
      }

      await buildTemplateFile(Component, templateName, false); // HTML
      await buildTemplateFile(Component, templateName, true); // Text

      console.log(`✓ Built ${templateName}`);
    } catch (error) {
      console.error(`✗ Error building ${templateName}:`, error);
    }
  }
}

async function main() {
  await discoverAndBuildTemplates();
  console.log('All templates built successfully!');
}

main().catch(console.error);
