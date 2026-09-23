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
          SelectVibeFile(): Promise<string>
          SelectNewVibeFile(): Promise<string>
          OpenFile(path: string, passphrase: string): Promise<void>
          CreateFile(path: string, passphrase: string, title: string, desc: string): Promise<void>
          ListTemplates(): Promise<TemplateInfo[]>
          CreateFromTemplate(path: string, passphrase: string, template: string): Promise<void>
          CloseFile(): Promise<void>
          SaveFile(): Promise<void>
          IsFileOpen(): Promise<boolean>
          GetFileInfo(): Promise<FileInfo>
          ListSkills(): Promise<SkillSummary[]>
          SaveSkill(name: string, description: string, instructions: string, tableName: string): Promise<void>
          DeleteSkill(name: string): Promise<void>
          ListPages(): Promise<PageSummary[]>
          GetPage(id: string): Promise<PageDetail>
          RenderPageContent(id: string, params: Record<string, string>): Promise<string>
          CreatePage(title: string, content: string): Promise<string>
          CreateHTMLPage(title: string, content: string): Promise<string>
          UpdatePage(id: string, title: string, content: string): Promise<void>
          DeletePage(id: string): Promise<void>
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
          AddColumn(table: string, column: string, colType: string): Promise<void>
          DropColumn(table: string, column: string): Promise<void>
          AddIndex(table: string, indexName: string, columns: string[], unique: boolean): Promise<void>
          AlterColumnType(table: string, column: string, newType: string): Promise<void>
          ListQueries(): Promise<QuerySummary[]>
          GetQuery(id: string): Promise<QueryDetail>
          SaveQuery(id: string, title: string, sql: string, engine: string, description: string): Promise<void>
          DeleteQuery(id: string): Promise<void>
          GetDashboard(): Promise<DashboardData>
          SendAgentQuery(query: string): Promise<AgentResult>
          GetAgentStatus(): Promise<Record<string, any>>
          SelectCSVFile(): Promise<string>
          ImportCSV(csvPath: string, tableName: string): Promise<ImportResult>
          ExportTableCSV(tableName: string): Promise<string>
          ExportTableJSON(tableName: string): Promise<string>
          GetAllMeta(): Promise<Record<string, string>>
          SetMeta(key: string, value: string): Promise<void>
          StartMCPServer(): Promise<string>
          StopMCPServer(): Promise<void>
          GetMCPStatus(): Promise<Record<string, any>>
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

// TemplateInfo mirrors studio.TemplateInfo (P6.7): a bundled starter-template
// descriptor surfaced to the Home/Unlock quick-start template picker cards.
interface TemplateInfo {
  name: string
  description: string
}

interface FileInfo {
  path: string
  title: string
  description: string
  page_count: number
  table_count: number
  query_count: number
  file_size: number
  // session_id identifies the currently open container (studio.App.sessionID).
  // A view that reads data at load time (NotebookView) should capture this
  // and send it back with any later write (SaveNotebook) so a write from a
  // superseded file session is rejected instead of landing in the wrong file.
  session_id: string
}

// SkillSummary mirrors studio.SkillSummary (#62): one Skill as the Skills
// view lists/renders it — built-in, user-authored, or import-generated.
interface SkillSummary {
  name: string
  description: string
  instructions: string
  table_name?: string
  source: string // 'built-in' | 'user' | 'import'
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
}

interface DashboardData {
  title: string
  description: string
  page_count: number
  table_count: number
  query_count: number
  file_size: number
  created_at: string
  updated_at: string
}

// A2UIQueryResult mirrors studio.A2UIQueryResult — the result of one A2UI
// data-bound component's live "sql" binding (RunA2UIQuery), gated through the
// agent sandbox's per-statement SQL classifier, not a bypass.
interface A2UIQueryResult {
  columns: string[]
  rows: any[][]
}

interface AgentResult {
  sql?: string
  columns?: string[]
  rows?: any[][]
  analysis?: string
  error?: string
}

interface ImportResult {
  rows_imported: number
  table_name: string
}

export {}
