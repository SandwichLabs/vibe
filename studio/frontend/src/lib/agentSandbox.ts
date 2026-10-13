// agentSandbox — the agent empty-state copy, keyed by the workspace's current
// sandbox mode (#91 M-F6/P-F2: the old copy was a fixed "can create tables"
// sentence, false whenever the mode was read-only/read-write), plus the one
// copy source for the Settings → Agent radio group (#79).

// SANDBOX_MODES is the ONE place the three sandbox modes' labels/help text
// are written — Settings' radio group builds itself from this list, so a
// label edit never has to be made in two places. value strings are exactly
// the Go mode strings (agents.SandboxMode).
export const SANDBOX_MODES = [
  { value: 'read-only', label: 'Look only', help: "Reads your tables and runs read-only SQL. Can't change anything." },
  { value: 'read-write', label: 'Change rows', help: "Can also add, edit and delete rows, add notebook cells and save skills. Can't create or drop tables, or save queries." },
  { value: 'full', label: 'Full control', help: 'Can also create, change and drop tables, save notebooks, queries and skills, and read or write files on this computer through SQL.' },
] as const

export function sandboxIntro(mode: string, ask: boolean = true): string {
  switch (mode) {
    case 'read-only':
      return "Read-only: the agent can read your schema and run read-only SQL. It can't change anything in this workspace."
    case 'read-write':
      return "Read-write: the agent can read your schema, run SQL, add, change or delete rows, add notebook cells and save skills. It can't create or drop tables, or save queries." + writeSuffix(ask)
    case 'full':
      return 'Full access: the agent can read and change rows, create, alter and drop tables, and save queries, notebooks and skills.' + writeSuffix(ask)
    default:
      return 'Ask about your data or ask the agent to build something. Every step it takes is shown below.'
  }
}

// writeSuffix appends the "ask before changes" state to a write-capable
// mode's sentence — read-only never writes anything, so it gets no suffix
// at all (see sandboxIntro's read-only case).
function writeSuffix(ask: boolean): string {
  return ask ? ' It asks you before each change.' : ' It makes changes without asking.'
}
