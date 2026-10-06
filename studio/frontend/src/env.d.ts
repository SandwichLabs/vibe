/// <reference types="vite/client" />

declare module '*.vue' {
  import type { DefineComponent } from 'vue'
  const component: DefineComponent<{}, {}, any>
  export default component
}

// Wails runtime bindings
declare global {
  interface Window {
    go: {
      main: {
        App: {
          GetPreferences(): Promise<{ theme: string; systemLight: string; systemDark: string; reduceMotion: boolean; uiScale: number }>
          SetPreferences(p: { theme: string; systemLight: string; systemDark: string; reduceMotion: boolean; uiScale: number }): Promise<void>
          ListUserThemes(): Promise<{ themes: { path: string; filename: string; json: string }[] | null; problems: string[] | null; dir: string }>
          OpenThemesDir(): Promise<string>
          // Workspaces (#111)
          GetInitialWorkspace(): Promise<{ path: string; message: string }>
          ListRecentWorkspaces(): Promise<RecentWorkspace[]>
          RemoveRecentWorkspace(path: string): Promise<RecentWorkspace | null>
          RestoreRecentWorkspace(r: RecentWorkspace): Promise<void>
          RelocateRecentWorkspace(oldPath: string, newPath: string): Promise<void>
          ChooseFolder(title: string): Promise<string>
          SelectWorkspaceFolder(): Promise<string>
          CheckGitAvailable(): Promise<boolean>
          CheckWorkspaceDir(parentDir: string, name: string): Promise<WorkspaceDirCheck>
          OpenWorkspace(path: string): Promise<OpenWorkspaceResult>
          OpenWorkspaceReadOnly(path: string): Promise<OpenWorkspaceResult>
          SwitchWorkspace(req: { path: string }): Promise<OpenWorkspaceResult>
          CreateWorkspace(req: CreateWorkspaceRequest): Promise<OpenWorkspaceResult>
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
          ListCatalogTables(connection: string): Promise<string[]>
          StartMCP(): Promise<MCPStatus>
          StopMCP(): Promise<void>
          GetMCPStatus(): Promise<MCPStatus>
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
          ExecuteQuery(sql: string): Promise<QueryResult>
          RunA2UIQuery(sql: string): Promise<A2UIQueryResult>
          RunA2UIQueryIn(sql: string, connection: string): Promise<A2UIQueryResult>
          AddColumn(table: string, column: string, colType: string): Promise<void>
          DropColumn(table: string, column: string): Promise<void>
          AddIndex(table: string, indexName: string, columns: string[], unique: boolean): Promise<void>
          AlterColumnType(table: string, column: string, newType: string): Promise<void>
          ListQueries(): Promise<QuerySummary[]>
          GetQuery(id: string): Promise<QueryDetail>
          SaveQuery(id: string, title: string, sql: string, description: string, rev: string): Promise<{ id: string; rev: string }>
          DeleteQuery(id: string): Promise<void>
          GetDashboard(): Promise<DashboardData>
          SendAgentQuery(query: string): Promise<AgentResult>
          GetAgentStatus(): Promise<Record<string, any>>
          GetAgentConfig(): Promise<AgentConfig>
          SetAgentConfig(cfg: AgentConfig): Promise<void>
          ResolveAgentApproval(id: string, approved: boolean): Promise<void>
          SelectImportFile(): Promise<string>
          InspectImport(src: ImportSource): Promise<ImportInspection>
          ImportFile(o: ImportOptions): Promise<ImportSummary>
          ExportTableCSV(tableName: string): Promise<string>
          ExportTableJSON(tableName: string): Promise<string>
        }
      }
    }
  }
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

interface ConnectionInfo {
  name: string
  type: string
  scope: 'shared' | 'local' | 'user'
  shadows?: string[]
  readOnly: boolean
  default: boolean
  status: string
  detail?: string
  lockedByPid?: number
}

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
}
interface ConflictResult { id: string; rev: string; title: string }

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

interface AgentResult {
  sql?: string
  columns?: string[]
  rows?: any[][]
  analysis?: string
  error?: string
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

export {}
