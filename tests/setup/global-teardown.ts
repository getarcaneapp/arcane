import { execSync } from 'child_process';
import path from 'node:path';
import fs from 'node:fs';
import { fileURLToPath } from 'node:url';

const __filename = fileURLToPath(import.meta.url);
const __dirname = path.dirname(__filename);

async function globalTeardown() {
  console.log('\nStarting global teardown...');

  const composeFile =
    process.env.COMPOSE_FILE ? path.resolve(__dirname, '..', process.env.COMPOSE_FILE) : path.resolve(__dirname, 'compose.yaml');
  const projectsDir = path.resolve(__dirname, 'projects');

  // 1. Stop and remove Docker containers
  try {
    console.log('Stopping Docker containers...');
    execSync(`docker compose -f ${composeFile} down -v`, { stdio: 'inherit' });
    console.log('Docker containers stopped and volumes removed.');
  } catch (error) {
    console.error('Warning: Failed to stop Docker containers cleanly:', error);
  }

  // 2. Clean up test projects created on disk
  try {
    if (fs.existsSync(projectsDir)) {
      console.log('Cleaning up test projects...');
      const items = fs.readdirSync(projectsDir);

      for (const item of items) {
        const itemPath = path.join(projectsDir, item);

        // Keep .env.global and test-project-static if they exist, remove everything else that looks like a test project
        if (item === '.env.global' || item === 'test-project-static') continue;

        if (fs.lstatSync(itemPath).isDirectory()) {
          fs.rmSync(itemPath, { recursive: true, force: true });
          console.log(`   - Removed directory: ${item}`);
        } else if (item.startsWith('test-project-')) {
          fs.unlinkSync(itemPath);
          console.log(`   - Removed file: ${item}`);
        }
      }
    }
  } catch (error) {
    console.error('Warning: Failed to clean up projects directory:', error);
  }

  console.log('Global teardown complete.\n');
}

export default globalTeardown;
