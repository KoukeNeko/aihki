import { test } from 'node:test'
import assert from 'node:assert/strict'
import {
  mkdtempSync,
  mkdirSync,
  writeFileSync,
  readFileSync,
  rmSync,
  existsSync,
} from 'node:fs'
import { tmpdir } from 'node:os'
import { join } from 'node:path'
import { assemblePages } from './assemble-pages.mjs'

function fixture(t) {
  const root = mkdtempSync(join(tmpdir(), 'taiga-pages-'))
  t.after(() => rmSync(root, { recursive: true, force: true }))
  const dist = join(root, 'dist'),
    site = join(root, 'site')
  mkdirSync(dist)
  mkdirSync(join(site, 'deb/dists'), { recursive: true })
  mkdirSync(join(site, 'rpm/repodata'), { recursive: true })
  for (const file of [
    'deb/dists/Release',
    'rpm/repodata/repomd.xml',
    'taiga-cli.gpg.key',
  ])
    writeFileSync(join(site, file), `original:${file}`)
  writeFileSync(join(dist, 'index.html'), '<h1>Taiga CLI</h1>')
  return { dist, site }
}
test('product page preserves package metadata and signing key byte for byte', (t) => {
  const { dist, site } = fixture(t)
  writeFileSync(join(site, 'index.html'), 'old landing page')
  assemblePages(dist, site)
  assert.equal(
    readFileSync(join(site, 'index.html'), 'utf8'),
    '<h1>Taiga CLI</h1>',
  )
  for (const file of [
    'deb/dists/Release',
    'rpm/repodata/repomd.xml',
    'taiga-cli.gpg.key',
  ])
    assert.equal(readFileSync(join(site, file), 'utf8'), `original:${file}`)
})
test('rejects reserved paths before copying any website files', (t) => {
  const { dist, site } = fixture(t)
  writeFileSync(join(dist, 'taiga-cli.gpg.key'), 'replacement')
  assert.throws(() => assemblePages(dist, site), /reserved path/)
  assert.equal(existsSync(join(site, 'index.html')), false)
})
test('refuses publication when package content is missing', (t) => {
  const { dist, site } = fixture(t)
  rmSync(join(site, 'rpm'), { recursive: true })
  assert.throws(() => assemblePages(dist, site), /Missing package repository/)
})
test('refuses to overwrite other existing site content', (t) => {
  const { dist, site } = fixture(t)
  writeFileSync(join(site, 'hero.png'), 'existing')
  writeFileSync(join(dist, 'hero.png'), 'new')
  assert.throws(() => assemblePages(dist, site), /existing Pages content/)
  assert.equal(readFileSync(join(site, 'hero.png'), 'utf8'), 'existing')
})
