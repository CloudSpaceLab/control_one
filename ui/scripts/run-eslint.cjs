const { spawnSync } = require('node:child_process');
const path = require('node:path');

const isWindows = process.platform === 'win32';
const command = isWindows ? 'npx eslint "src/**/*.{ts,tsx}"' : 'npx';
const args = isWindows ? [] : ['eslint', 'src/**/*.{ts,tsx}'];

const result = spawnSync(command, args, {
  cwd: path.resolve(__dirname, '..'),
  env: { ...process.env, ESLINT_USE_FLAT_CONFIG: 'false' },
  shell: isWindows,
  stdio: 'inherit',
});

if (result.error) {
  console.error(result.error.message);
}
process.exit(result.status ?? 1);
