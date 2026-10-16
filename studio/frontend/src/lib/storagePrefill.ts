// The prefill of the new-connection form from a link (#130 step 3): `Set up
// storage` on a lake form opens `#/connections/new?type=s3&prefix=s3://lake/&
// name=lake_s3&in=local&return=lake-draft`. Anything can navigate to such a
// link, so the query is checked here and a prefill is applied whole or not at
// all. It only ever fills visible fields: it never saves, never picks a sign-in
// method, and `in` is limited to the person's own scopes.
import { NAME_RE } from './connections'

// The `return` value that means "back to the lake form this came from".
export const RETURN_LAKE = 'lake-draft'

// The URL schemes a scope of each storage type may use. A courtesy copy of Go's
// connections.StorageTypeForScheme: keep them in step (Go is the authority and
// validates again on Save).
const SCHEMES: Record<string, string[]> = {
  s3: ['s3://', 's3a://', 's3n://'],
  gcs: ['gcs://', 'gs://'],
  r2: ['r2://'],
  azure: ['az://', 'azure://', 'abfss://'],
}

export interface StoragePrefill {
  type: string
  prefix: string
  name: string
  scope: ConnScope
}

// prefixProblem is why a scope prefix is not acceptable for a type ('' when it is).
// It is a courtesy check on the same rules Go applies on Save: a plain URL of
// the type's scheme with a bucket, no query, glob, dot segment or control byte.
export function prefixProblem(type: string, prefix: string): string {
  const schemes = SCHEMES[type]
  if (!schemes) return 'not a storage type'
  if (prefix.length === 0 || prefix.length > 2048) return 'bad length'
  if (/[\u0000-\u0020\u007f\\?*[\]#]/.test(prefix)) return 'not a plain URL'
  if (/%2e/i.test(prefix) || prefix.split('/').includes('..') || prefix.split('/').includes('.')) return 'dot segment'
  const scheme = schemes.find((s) => prefix.startsWith(s))
  if (!scheme) return 'wrong scheme'
  // user:pass@host is never a scope; only Azure's container@account form has an @.
  if (type !== 'azure' && prefix.slice(scheme.length).split('/')[0].includes('@')) return 'userinfo'
  if (prefix.slice(scheme.length).split('/')[0] === '') return 'no bucket'
  return ''
}

// parseStoragePrefill reads the prefill out of a route query. null means there
// is none, or that something in it is invalid (nothing is half-applied).
export function parseStoragePrefill(query: Record<string, unknown>): StoragePrefill | null {
  const str = (k: string): string | null | undefined => {
    const v = query[k]
    if (v === undefined) return undefined
    return typeof v === 'string' ? v : null // an array or anything else is invalid
  }
  const type = str('type')
  const prefix = str('prefix')
  const name = str('name')
  const scope = str('in')
  if (type === undefined && prefix === undefined) return null
  if (!type || !prefix || prefixProblem(type, prefix)) return null
  if (name === null || (name !== undefined && !NAME_RE.test(name))) return null
  if (scope === null || (scope !== undefined && scope !== 'local' && scope !== 'user')) return null
  return { type, prefix, name: name ?? '', scope: scope ?? 'local' }
}
