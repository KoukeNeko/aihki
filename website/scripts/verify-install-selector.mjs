import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import { join } from 'node:path'

const app = readFileSync(join(process.cwd(), 'src/App.svelte'), 'utf8')

assert.match(app, /const activeInstall = \$derived\(installs\[platform\]\)/)
assert.match(app, /class:active=\{platform === i\}/)
assert.match(app, /copy\(activeInstall\.command, 'hero'\)/)
assert.match(app, /Copy \$\{activeInstall\.name\} install command/)

console.log('Install selector contract: PASS')