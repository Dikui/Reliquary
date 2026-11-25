import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import { join } from 'node:path'
import test from 'node:test'

// Run from frontend/ (npm test); the repository root is one level up.
const root = join(process.cwd(), '..')

// Docker builds run `npm ci` with an empty cache, so every lockfile URL is fetched.
// A mirror host baked into a lockfile breaks builds wherever that mirror is
// unreachable; registry.npmjs.org URLs follow the build's npm registry setting.
test('lockfiles resolve packages from the npm registry only', () => {
  for (const lockfile of ['frontend', 'showcase', 'agents/chat', 'agents/pi'].map((dir) => join(dir, 'package-lock.json'))) {
    const hosts = new Set(
      [...readFileSync(join(root, lockfile), 'utf8').matchAll(/"resolved": "https:\/\/([^/"]+)\//g)].map(([, host]) => host),
    )
    assert.deepEqual([...hosts], ['registry.npmjs.org'], lockfile)
  }
})

// The backend treats a request as the sole local user only when the browser's Host
// is loopback (internal/middleware/auth.go). A proxy that sends its upstream name
// as Host gets 401, so the showcase must forward the same headers as the admin UI.
test('the showcase API proxy forwards the same request headers as the admin proxy', () => {
  const headers = (text: string) => [...text.matchAll(/proxy_set_header\s+(Host|X-Real-IP|X-Forwarded-For|X-Forwarded-Proto)\s+([^;]+);/g)]
    .map(([, name, value]) => `${name} ${value}`).sort()
  const admin = headers(readFileSync(join(root, 'frontend/nginx-api-proxy.conf'), 'utf8'))
  const showcaseConf = readFileSync(join(root, 'showcase/nginx.conf'), 'utf8')
  const shelfLocation = /location = \/api\/v1\/showcase\/shelf \{([\s\S]*?)\n    \}/.exec(showcaseConf)?.[1]
  assert.ok(shelfLocation, 'showcase/nginx.conf must define the shelf API location')
  assert.equal(admin.length, 4)
  assert.deepEqual(headers(shelfLocation), admin)
})
