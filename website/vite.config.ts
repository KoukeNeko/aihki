import { svelte } from '@sveltejs/vite-plugin-svelte'
import { defineConfig } from 'vite'
import { readFileSync } from 'node:fs'

const compatibility = readFileSync(
  new URL('../COMPATIBILITY.md', import.meta.url),
  'utf8',
)
const verifiedServers = [
  ...compatibility.matchAll(/^\| (Taiga \d[^|]*?)\s*\| Verified \|/gm),
].map((match) => match[1].trim())
if (!verifiedServers.length)
  throw new Error('COMPATIBILITY.md must list a verified Taiga server')

// https://vite.dev/config/
export default defineConfig({
  plugins: [svelte()],
  base: './',
  define: { __VERIFIED_SERVERS__: JSON.stringify(verifiedServers.join(', ')) },
})
