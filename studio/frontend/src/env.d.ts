/// <reference types="vite/client" />

declare module '*.vue' {
  import type { DefineComponent } from 'vue'
  const component: DefineComponent<{}, {}, any>
  export default component
}

// The typed shape of the Wails-bound Go App (every exported method on
// studio.App). Hand-written; kept honest by two checks: scripts/check-bindings.sh
// (every Go method is declared here and has a caller) and
// studio/frontend/scripts/check-arity.mjs (CI build-smoke: parameter counts match
// the wails-generated App.d.ts). The ONLY runtime access to window.go is
// src/lib/bindings.ts.
interface AppApi {
  // About (#40)
  GetBuildInfo(): Promise<BuildInfo>
  GetThirdPartyNotices(): Promise<string>
  GetPreferences(): Promise<{ theme: string; systemLight: string; systemDark: string; reduceMotion: boolean; uiScale: number }>
  SetPreferences(p: { theme: string; systemLight: string; systemDark: string; reduceMotion: boolean; uiScale: number }): Promise<void>
  ListUserThemes(): Promise<{ themes: { path: string; filename: string; json: string }[] | null; problems: string[] | null; dir: string }>
  OpenThemesDir(): Promise<string>
  // Updates (Settings > About > Updates, the first-run prompt)
  GetUpdateState(): Promise<UpdateState>
  SetUpdateCheck(mode: string): Promise<void>
  CheckForUpdate(): Promise<UpdateResult>
  AutoCheckForUpdate(): Promise<UpdateResult | null>
  SkipUpdate(tag: string): Promise<void>
  ApplyUpdate(tag: string): Promise<UpdateApplyResult>
  RestartAfterUpdate(): Promise<void>
  // Workspaces (#111)
  GetInitialWorkspace(): Promise<{ path: string; message: string; bundlePath: string }>
  MarkLaunchHandled(): Promise<void>
  ListRecentWorkspaces(): Promise<RecentWorkspace[]>
  RemoveRecentWorkspace(path: string): Promise<RecentWorkspace | null>
  RestoreRecentWorkspace(r: RecentWorkspace): Promise<void>
  RelocateRecentWorkspace(oldPath: string, newPath: string): Promise<void>
  ChooseFolder(title: string): Promise<string>
  SelectWorkspaceFolder(): Promise<string>
  CheckGitAvailable(): Promise<boolean>
  CheckWorkspaceDir(parentDir: string, name: string): Promise<WorkspaceDirCheck>
  OpenWorkspace(path: string): Promise<OpenWorkspaceResult>
  CreateWorkspace(req: CreateWorkspaceRequest): Promise<OpenWorkspaceResult>
  // .vibe bundles (#118)
  GetBundleContent(): Promise<BundleContent>
  SelectBundleFile(): Promise<string>
  ExportBundle(req: BundleExportRequest): Promise<BundleExportResult>
  PreviewBundleImport(path: string, passphrase: string): Promise<BundlePreview>
  CommitBundleImport(handle: string, d: BundleDecisions): Promise<BundleResult>
  ImportBundleIntoNewWorkspace(handle: string, req: CreateWorkspaceRequest, d: BundleDecisions): Promise<{ opened: OpenWorkspaceResult; result: BundleResult }>
  DiscardBundleImport(handle: string): Promise<void>
  CloseWorkspace(): Promise<void>
  TakeOverWorkspace(): Promise<OpenWorkspaceResult>
  GetWorkspaceInfo(): Promise<WorkspaceInfo>
  GetSessionID(): Promise<string>
  IsWorkspaceOpen(): Promise<boolean>
  SetWorkspaceSettings(s: WorkspaceSettings): Promise<WorkspaceInfo>
  RegenerateWorkspaceID(): Promise<WorkspaceInfo>
  TrustLocalSettings(): Promise<WorkspaceInfo>
  ResolveConflict(req: ConflictRequest): Promise<ConflictResult>
  ListTemplates(): Promise<TemplateInfo[]>
  ListCatalogGroups(): Promise<CatalogGroup[]>
  // Connections (#117)
  ListConnectionTypes(): Promise<ConnTypeSpec[]>
  ListConnections(): Promise<ConnectionList>
  GetConnection(name: string): Promise<ConnectionDetail>
  GetConnectionInScope(ref: ConnectionRef): Promise<ConnectionDetail>
  ConnectionFingerprint(def: ConnectionDef): Promise<string>
  SaveConnection(req: SaveConnectionRequest): Promise<ConnectionDetail>
  MoveConnection(req: { name: string; to: ConnScope }): Promise<ConnectionDetail>
  DeleteConnection(ref: ConnectionRef): Promise<void>
  TestConnection(req: TestConnectionRequest): Promise<TestResultView>
  ListMotherDuckDatabases(req: ListMotherDuckDatabasesRequest): Promise<MotherDuckDatabasesView>
  ConnectConnection(ref: ConnectionRef): Promise<ConnectionInfo | null>
  ConnectWithCredentials(req: ConnectWithCredentialsRequest): Promise<ConnectionInfo | null>
  DisconnectConnection(ref: ConnectionRef): Promise<ConnectionInfo | null>
  RefreshCatalog(ref: ConnectionRef): Promise<void>
  SetConnectionCredentials(req: SetCredentialsRequest): Promise<ConnectionInfo | null>
  ClearConnectionCredentials(req: { name: string; field: string }): Promise<ConnectionInfo | null>
  GetConnectionCredentialStatus(name: string): Promise<ConnCredentialStatus>
  ListCatalog(req: { connection: string; schema: string; table: string }): Promise<CatalogListing>
  GetTableCapabilities(table: string): Promise<TableCapabilities>
  InstallExtension(req: { name: string }): Promise<void>
  ListExtensions(): Promise<ExtensionInfo[]>
  StartMCP(): Promise<MCPStatus>
  StopMCP(): Promise<void>
  GetMCPStatus(): Promise<MCPStatus>
  // The workspace's .env (#168): names and state only, never values
  GetDotEnvStatus(): Promise<DotEnvStatus>
  AllowDotEnv(): Promise<DotEnvStatus>
  DenyDotEnv(): Promise<DotEnvStatus>
  RevokeDotEnv(): Promise<DotEnvStatus>
  GetClaudeConfig(): Promise<{ name: string; desktop: string; code: string }>
  CancelQuit(): Promise<void>
  HoldQuit(): Promise<void>
  Quit(): Promise<void>
  ListSkills(): Promise<SkillSummary[]>
  SaveSkill(name: string, description: string, instructions: string, tableName: string, rev: string): Promise<void>
  DeleteSkill(name: string): Promise<void>
  ListTables(): Promise<string[]>
  DescribeTable(name: string): Promise<TableInfo>
  GetTableData(name: string, page: number, pageSize: number, sortCol: string, sortDir: string): Promise<TableData>
  GetTableDataFiltered(name: string, spec: FilterSpec, page: number, pageSize: number, sortCol: string, sortDir: string): Promise<TableData>
  SaveFilterAsQuery(name: string, spec: FilterSpec, columns: string[], title: string): Promise<string>
  InsertRow(table: string, values: Record<string, any>): Promise<WriteResult>
  UpdateRow(table: string, pkCol: string, pkVal: any, values: Record<string, any>): Promise<WriteResult>
  DeleteRow(table: string, pkCol: string, pkVal: any): Promise<WriteResult>
  CreateTable(name: string, columns: ColumnInfo[]): Promise<void>
  DropTable(name: string): Promise<void>
  RunQuery(req: { sql: string; connection: string; tabId: string }): Promise<QueryResult>
  RunA2UIQuery(sql: string): Promise<A2UIQueryResult>
  RunA2UIQueryIn(sql: string, connection: string): Promise<A2UIQueryResult>
  ListQueries(): Promise<QuerySummary[]>
  GetQuery(id: string): Promise<QueryDetail>
  SaveQueryOn(req: { id: string; title: string; sql: string; description: string; rev: string; connection: string }): Promise<{ id: string; rev: string }>
  DeleteQuery(id: string): Promise<void>
  GetDashboard(): Promise<DashboardData>
  GetAgentConfig(): Promise<AgentConfig>
  ProbeKeychain(): Promise<KeychainProbe>
  GetLLMKeyStatus(provider: string): Promise<LLMKeyStatus>
  GetLLMSettings(): Promise<LLMSettingsView>
  SetLLMSettings(s: LLMSettingsInput): Promise<void>
  ListLLMModels(provider: string, endpoint: string): Promise<LLMModelList>
  SetLLMKey(provider: string, key: string): Promise<void>
  ClearLLMKey(provider: string): Promise<void>
  MigrateLLMKeyToKeychain(overwrite: boolean): Promise<void>
  ResolveAgentApproval(id: string, approved: boolean): Promise<void>
  SelectImportFile(): Promise<string>
  InspectImport(src: ImportSource): Promise<ImportInspection>
  ImportFile(o: ImportOptions): Promise<ImportSummary>
  // Agent chat and task mode (#63, #66)
  AgentChatStream(turnId: string, message: string, pinnedContext: string, history: { role: string; text: string }[]): Promise<AgentChatResult>
  AgentTask(req: { taskId: string; mode: string; description: string; cell?: import('./composables/useAiCellDialog').TaskCellArg; followUp?: { role: string; text: string }[]; tables?: string[]; notebookSummary?: string[] }): Promise<import('./composables/useAiCellDialog').TaskEnvelope>
  CancelAgentTask(taskId: string): Promise<void>
  CancelAgentQuery(turnId: string): Promise<void>
  // Notebooks (#66)
  ListNotebooks(): Promise<WailsNotebookSummary[]>
  GetNotebook(id: string): Promise<WailsNotebook>
  CreateNotebook(title: string): Promise<WailsNotebook>
  DeleteNotebook(id: string): Promise<void>
  SaveNotebook(nb: Record<string, any>, sessionId: string): Promise<{ notebook: WailsNotebook; warnings: string[] | null }>
  SelectNotebookFile(): Promise<string>
  ImportNotebook(path: string): Promise<{ notebook: WailsNotebook; warnings: string[] | null }>
  ExportNotebook(id: string, dst: string): Promise<string>
  SetActiveNotebook(id: string): Promise<void>
  // Exports
  ExportQueryResults(sql: string, format: string, dst: string): Promise<string>
}

// A notebook as the bindings return it: the editor owns the cell shapes, so
// only the envelope is typed here.
interface WailsNotebook {
  id: string
  title: string
  rev?: string
  cells: any[]
  [key: string]: any
}

interface WailsNotebookSummary {
  id: string
  title: string
  cellCount: number
  updatedAt: string
  error?: string
}

interface AgentChatResult {
  text: string
  steps: { tool: string; input: string; output: string; isError?: boolean }[]
}

// WriteResult is returned by the transactional CRUD methods
// (InsertRow/UpdateRow/DeleteRow) and reports the number of affected rows.
interface WriteResult {
  affected: number
}

// WriteError is the structured, field-keyed CRUD failure surfaced by the
// backend so the form UI can highlight the offending controls. It arrives as a
// rejected promise (JSON-encoded) rather than the resolved value.
interface WriteError {
  message: string
  fields?: Record<string, string>
}

// TemplateInfo mirrors studio.TemplateInfo: a bundled starter-template
// descriptor surfaced to the New workspace dialog's "Start from" cards.
interface TemplateInfo {
  name: string
  description: string
}

// --- Workspaces (#111): mirrors studio/workspace.go ---

interface RecentWorkspace {
  path: string
  id: string
  name: string
  lastOpened: string
  gitBranch: string
  missing: boolean
}

type ConnScope = 'shared' | 'local' | 'user'

interface ConnectionInfo {
  name: string
  type: string
  scope: ConnScope
  shadows?: string[]
  readOnly: boolean
  default: boolean
  status: string
  detail?: string
  lockedByPid?: number
  remote: boolean
  catalogless?: boolean
  signIn?: string
  extension?: string
  serverChanged?: { old: string; new: string }
}

// --- Connections (#117): mirrors studio/connections.go. No field here is a
// secret; the password, token and secret key are write-only. ---

interface ConnField {
  name: string
  label: string
  kind: 'text' | 'number' | 'enum' | 'path' | 'url'
  help?: string
  default?: string
  choices?: string[]
  required: boolean
  secret: boolean
  perPerson: boolean
  option?: boolean
  prominent?: boolean
}
interface ConnTypeSpec {
  id: string
  label: string
  description: string
  fields: ConnField[]
  credentials: ConnField[]
  requiresExtension: string[]
  capabilities: { catalog: boolean; writable: boolean; readOnlyAttach: boolean; remote: boolean; test: boolean; subprocess?: boolean }
  fingerprintFields: string[]
  signInMethods: string[]
  soon: boolean
}
interface ConnectionList {
  connections: ConnectionInfo[]
  relaunchRequired: boolean
  relaunchMessage?: string
}
interface ConnectionRef { name: string; scope: ConnScope | '' }
interface ConnectionDef {
  name: string
  type: string
  path?: string
  host?: string
  port?: number
  database?: string
  region?: string
  scope?: string
  read_only?: boolean | null
  credentials?: string
  options?: Record<string, string>
}
interface ConnCredentialStatus {
  method: string
  username?: string
  keyId?: string
  secretSet: boolean
  sessionOnly?: boolean
  fingerprintMatches: boolean
  available: boolean
  detail?: string
  env?: { name: string; set: boolean }[]
}
interface ConnectionDetail {
  name: string
  scope: ConnScope
  def: ConnectionDef
  fingerprint: string
  shadows: string[]
  shadowedBy?: string
  signIn: string
  signInHint?: string
  credential: ConnCredentialStatus
  info: ConnectionInfo
}
interface SaveConnectionRequest { scope: ConnScope; def: ConnectionDef; isNew: boolean; signIn: string; confirmRebind?: boolean }
interface CredentialInput { username: string; keyId: string; password: string; token: string; secret: string }
interface SetCredentialsRequest extends CredentialInput { name: string; method: string; remember: boolean; confirmRebind?: boolean }
interface ConnectWithCredentialsRequest extends CredentialInput { name: string; remember: boolean }
interface TestConnectionRequest { scope: ConnScope; def: ConnectionDef; signIn: string; credentials: CredentialInput }
interface ListMotherDuckDatabasesRequest { name: string; scope: ConnScope; signIn: string; credentials: CredentialInput; install: boolean }
interface MotherDuckDatabase { name: string; share: boolean }
interface MotherDuckDatabasesView { ok: boolean; status: string; databases: MotherDuckDatabase[]; source?: string; message?: string; installExtension?: string }
interface TestResultView {
  ok: boolean
  status: string
  ms: number
  count: number
  detail?: string
  summary?: string
  error?: { code: string; message: string; details: string }
  installExtension?: string
}
interface CatalogNode { kind: 'schema' | 'table' | 'view' | 'column'; name: string; qualified?: string; rows: number; dataType?: string; pk?: boolean; fk?: boolean }
interface CatalogListing { kind: 'schemas' | 'tables' | 'columns'; schema: string; flattened: boolean; nodes: CatalogNode[] }
interface TableCapabilities { connection: string; editable: boolean; reason: string; text: string }
interface ExtensionInfo { name: string; purpose: string; neededBy: string[]; installed: boolean }

interface AgentPolicyInfo {
  sandbox: string
  askBeforeWrites: boolean
}

interface HolderInfo {
  app: string
  pid: number
  servesMcp: boolean
  text: string
}

interface WorkspaceInfo {
  root: string
  id: string
  name: string
  sessionId: string
  gitBranch: string
  isGit: boolean
  defaultConnection: string
  connections: ConnectionInfo[]
  counts: { notebooks: number; queries: number; skills: number; tables: number }
  policy: AgentPolicyInfo
  team: AgentPolicyInfo
  local: AgentPolicyInfo
  teamSandboxSet: boolean
  readOnly: boolean
  holder?: HolderInfo
  issues: { connection: string; message: string }[]
  warnings: string[]
  duplicateIds: string[]
  localUntrusted: boolean
  mcpRunning: boolean
}

interface OpenWorkspaceResult {
  status: 'opened' | 'not-workspace'
  path: string
  info?: WorkspaceInfo
  warnings?: string[]
}

interface CreateWorkspaceRequest {
  name: string
  dir: string
  parentDir: string
  git: boolean
  template: string
  inPlace: boolean
}

interface WorkspaceDirCheck { dir: string; exists: boolean; notEmpty: boolean }

interface WorkspaceSettings {
  name: string
  defaultConnection: string
  teamSandbox: string
  teamAskBeforeChanges: boolean
  sandbox: string
  askBeforeChanges: boolean
  loadedTeamSandbox?: string
  loadedTeamAsk?: boolean
}

interface ConflictRequest {
  kind: 'notebook' | 'query' | 'skill'
  id: string
  action: 'reload' | 'overwrite' | 'saveCopy'
  data: Record<string, any>
  rev?: string
  connection?: string
}
interface ConflictResult { id: string; rev: string; title: string }

interface DotEnvSatisfies { connection: string; signIn: string; variables: string[] }
interface DotEnvStatus {
  state: 'none' | 'undecided' | 'allowed' | 'changed' | 'denied'
  path: string
  names: string[]
  warnings?: string[]
  ignored?: string
  trackedByGit: boolean
  loaded: boolean
  satisfies: DotEnvSatisfies[]
}

interface MCPStatus { running: boolean; url: string; canStart: boolean; reason: string }

interface CatalogTable { name: string; qualified: string; kind: 'table' | 'view' }
interface CatalogGroup {
  connection: string
  type: string
  status: string
  detail?: string
  readOnly: boolean
  default: boolean
  tables: CatalogTable[]
}

// SkillSummary mirrors studio.SkillSummary (#62): one Skill as the Skills
// view lists/renders it — built-in, user-authored, or import-generated.
interface SkillSummary {
  name: string
  description: string
  instructions: string
  table_name?: string
  source: string // 'built-in' | 'user' | 'import'
  rev?: string // store revision as listed; sent back on save
}

interface PageSummary {
  id: string
  title: string
  page_type: string
  sort_order: number
  migrated_to: string
}

interface PageDetail {
  id: string
  title: string
  content: string
  page_type: string
  sort_order: number
}

interface ColumnInfo {
  name: string
  type: string
  not_null: boolean
  pk: boolean
  // fk is read-only output from DescribeTable (a real DuckDB FOREIGN KEY
  // constraint, read from duckdb_constraints() — #68 dropped the
  // _vibe_relationships metadata table); CreateTable/AddColumn ignore it.
  fk?: boolean
}

interface TableInfo {
  name: string
  columns: ColumnInfo[]
}

// FilterCondition / FilterSpec mirror studio.FilterCondition / studio.FilterSpec
// (the SQL-free datasheet visual-querying unit behind Filter-by-Selection and
// Filter-by-Form, plan §2.3 / P2.5).
interface FilterCondition {
  column: string
  op: string
  value?: any
}

interface FilterSpec {
  combinator: string
  conditions: FilterCondition[]
}

interface TableData {
  columns: string[]
  types: string[]
  rows: any[][]
  page: number
  page_size: number
  total_rows: number
  total_pages: number
}

interface QueryResult {
  columns: ColumnInfo[]
  rows: any[][]
  duration: string
  row_count: number
  total_rows: number
}

// KeychainProbe / LLMKeyStatus (#114): where the agent's API key comes from.
// There is no way to read the key itself; the bindings set, clear and report.
interface KeychainProbe {
  available: boolean
  backend: string
  detail?: string
}

interface LLMKeyStatus {
  provider: string
  source: 'env' | 'keychain' | 'file' | 'none'
  set: boolean
  fileKey: boolean
  canMigrate: boolean
  keychainAvailable: boolean
  keychainHasKey: boolean
  detail?: string
}

// Settings → App → "The agent's model" (app-global; no workspace needed).
interface LLMProviderInfo {
  id: string
  label: string
  description: string
  needsKey: boolean
  keyOptional: boolean
  keyLabel: string
  keyEnvVars: string[]
  hasEndpoint: boolean
  defaultEndpoint?: string
  hasAccount: boolean
  defaultModel: string
  suggestedModels: string[]
}

interface LLMSettingsInput {
  provider: string
  model: string
  endpoint: string
  accountId: string
  gateway: string
}

interface LLMSettingsView extends LLMSettingsInput {
  effectiveModel: string
  defaultModel: string
  accountIdFromEnv: boolean
  envOverrides: Record<string, string>
  providers: LLMProviderInfo[]
}

interface LLMModelList {
  models: string[] | null
  error?: string
  unreachable?: boolean
}

interface AgentConfig {
  provider: string
  model: string
  endpoint: string
  sandbox: string
  askBeforeWrites: boolean
  teamSandbox?: string
  teamAskBeforeWrites?: boolean
}

interface QuerySummary {
  id: string
  title: string
  engine: string
}

interface QueryDetail {
  id: string
  title: string
  sql: string
  engine: string
  parameters: any[]
  created_at: string
  updated_at: string
  rev?: string
}

interface DashboardData {
  title: string
  description: string
  notebook_count: number
  table_count: number
  query_count: number
  skill_count: number
  local_tables: string[]
  default_connection: string
  read_only: boolean
}

// A2UIQueryResult mirrors studio.A2UIQueryResult — the result of one A2UI
// data-bound component's live "sql" binding (RunA2UIQuery), gated through the
// agent sandbox's per-statement SQL classifier, not a bypass.
interface A2UIQueryResult {
  columns: string[]
  types: string[]
  rows: any[][]
}

// Mirrors internal/importer's Type/ColType/Source/Inspection/Column/Options/
// Summary/BadValue (#82). Keep in sync with internal/importer/*.go and
// lib/importTypes.ts.
type ImportFriendlyType = 'text' | 'integer' | 'decimal' | 'number' | 'date' | 'timestamp' | 'boolean'

interface ImportColType {
  type: ImportFriendlyType
  scale?: number
}

interface ImportSource {
  path: string
  sheet?: string
  has_header: boolean
}

interface ImportColumn {
  name: string
  source_name: string
  inferred: ImportColType
  candidate: boolean
}

interface ImportInspection {
  sheets?: string[]
  sheet?: string
  columns: ImportColumn[]
  rows: (string | null)[][]
  row_count: number
  suggested_key: string
}

type ImportMode = 'create' | 'replace' | 'append' | 'upsert'

interface ImportOptions extends ImportSource {
  table: string
  mode?: ImportMode | ''
  key?: string
  types?: Record<string, ImportColType>
  dry_run?: boolean
}

interface ImportBadValue {
  row: number
  column: string
  value: string
  want: string
  reason: 'type' | 'missing_parent' | 'duplicate_key' | 'null_key'
}

interface ImportSummary {
  ok: boolean
  mode: ImportMode | ''
  table: string
  exists: boolean
  existing_rows: number
  table_key: string
  modes: ImportMode[]
  rows: number
  inserted: number
  updated: number
  unchanged: number
  replaced: number
  exact_dupes: number
  missing: string[] | null
  extra: string[] | null
  bad_values: ImportBadValue[] | null
  bad_count: number
  blocked: string
}


// --- .vibe bundles (#118): mirrors studio/bundle.go and internal/bundle ---

interface BundleContentItem { id: string; title: string }
interface BundleContentConnection {
  name: string; type: string; summary: string
  scope: 'team' | 'this computer'
  portable: boolean; reason?: string
}
interface BundleContent {
  workspaceName: string
  notebooks: BundleContentItem[]
  queries: BundleContentItem[]
  skills: BundleContentItem[]
  connections: BundleContentConnection[]
}
interface BundleSelection {
  all: boolean
  notebooks: string[]; queries: string[]; skills: string[]
  sharedConnections: boolean
  teamConnections: string[]
  localConnections: string[]
}
interface BundleExportRequest { path: string; selection: BundleSelection; passphrase: string }
interface BundleExportResult {
  path: string; cancelled: boolean; encrypted: boolean
  notebooks: number; queries: number; skills: number; connections: number
  bytes: number; warnings: string[]
}
type BundleAction = 'rename' | 'overwrite' | 'skip'
interface BundlePreviewItem { key: string; kind: 'notebook' | 'query' | 'skill'; id: string; title: string; size: number; conflict: boolean }
interface BundlePreviewConnection {
  key: string; name: string; type: string; summary: string
  details: string[]; risks: string[]; askedWritable: boolean
  conflict: boolean; conflictScope?: string; renamedTo?: string
}
interface BundlePreview {
  handle: string
  needsPassphrase: boolean
  wrongPassphrase: boolean
  path: string
  workspaceName: string; createdAt: string; createdBy: string; encrypted: boolean
  items: BundlePreviewItem[]
  connections: BundlePreviewConnection[]
  warnings: string[]
  makerNotes: string[]
}
interface BundleDecisions { onConflict: BundleAction; items: Record<string, BundleAction>; connections: string[] }
interface BundleOutcome { key: string; kind: string; id: string; finalId?: string; result: string; error?: string }
interface BundleResult { outcomes: BundleOutcome[]; dryRun: boolean; warnings: string[] }

// Updates: the saved choice and install kind (no network), one check's result, and a finished upgrade.
interface UpdateState {
  check: '' | 'off' | 'weekly'; lastChecked: string; skippedVersion: string
  version: string; isDev: boolean; needsConsent: boolean; managed: string; updatable: boolean; managedNote: string
  appliedVersion: string; canRestart: boolean
}
interface UpdateResult {
  current: string; latest: string; updateAvailable: boolean; skipped: boolean; releaseUrl: string
  notes: string; managed: string; updatable: boolean; managedNote: string; checkedAt: string
}
interface UpdateApplyResult { from: string; to: string; output: string; studio: boolean; restart: string }

// Settings > About (#40): what GetBuildInfo returns.
interface BuildInfo { version: string; commit: string; date: string; goVersion: string; duckdbVersion: string; platform: string }
