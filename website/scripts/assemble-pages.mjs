import { existsSync, readdirSync, cpSync, statSync } from 'node:fs'
import { resolve, join } from 'node:path'
import { fileURLToPath } from 'node:url'

// Merge the product page into the package repository, never replace its root.
export function assemblePages(dist, site) {
  for (const required of ['deb/dists', 'rpm/repodata', 'taiga-cli.gpg.key']) {
    if (!existsSync(join(site, required)))
      throw new Error(`Missing package repository: ${required}`)
  }
  if (!existsSync(join(dist, 'index.html')))
    throw new Error('Build the website before assembling Pages')
  const entries = readdirSync(dist)
  for (const entry of entries) {
    if (['deb', 'rpm', 'taiga-cli.gpg.key', 'CNAME'].includes(entry)) {
      throw new Error(`Website must not replace reserved path: ${entry}`)
    }
    if (
      existsSync(join(site, entry)) &&
      !(entry === 'index.html' && statSync(join(site, entry)).isFile())
    ) {
      throw new Error(`Refusing to overwrite existing Pages content: ${entry}`)
    }
  }
  for (const entry of entries)
    cpSync(join(dist, entry), join(site, entry), { recursive: true })
}

if (
  process.argv[1] &&
  resolve(process.argv[1]) === fileURLToPath(import.meta.url)
) {
  if (process.argv.length !== 4)
    throw new Error(
      'Usage: node assemble-pages.mjs <dist> <existing-package-site>',
    )
  assemblePages(resolve(process.argv[2]), resolve(process.argv[3]))
}
