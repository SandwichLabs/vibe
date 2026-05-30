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
          GetAppConfig(): Promise<AppConfig>
          SetMode(mode: string): Promise<void>
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
          GetFieldMeta(table: string): Promise<FieldMeta[]>
          SetFieldMeta(table: string, column: string, meta: FieldMeta): Promise<void>
          AddColumn(table: string, column: string, colType: string): Promise<void>
          DropColumn(table: string, column: string): Promise<void>
          AddIndex(table: string, indexName: string, columns: string[], unique: boolean): Promise<void>
          AlterColumnType(table: string, column: string, newType: string): Promise<void>
          SaveForm(spec: FormSpec): Promise<void>
          GetForm(id: string): Promise<FormSpec>
          ListForms(): Promise<FormSummary[]>
          SubmitForm(parent: FormSubmitRow, children: FormSubmitRow[]): Promise<void>
          SubmitFormFor(formID: string, parent: FormSubmitRow, children: FormSubmitRow[]): Promise<void>
          UpdateRowFor(formID: string, pkCol: string, pkVal: any, values: Record<string, any>): Promise<WriteResult>
          FireFormOpen(formID: string): Promise<MacroRunResult>
          RunMacro(id: string): Promise<MacroRunResult>
          RunAutoExec(): Promise<MacroRunResult>
          SaveMacro(spec: any): Promise<void>
          GetMacro(id: string): Promise<any>
          ListMacro(): Promise<any[]>
          DeleteMacro(id: string): Promise<void>
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

// FieldMeta mirrors internal/schema.FieldMeta (Access-parity field properties).
interface FieldMeta {
  table_name: string
  column_name: string
  caption: string
  control_type: string
  default_value: string
  required: boolean
  format_code: string
  validation_rule: string
  validation_message: string
  input_mask: string
  lookup_sql: string
  display_col: string
  value_col: string
  control_tip: string
  sort_order: number
}

// FormSpec mirrors internal/schema.FormSpec (Access-parity form definition).
interface FormFieldSpec {
  name: string
  caption: string
  control_type: string
}

interface SubformSpec {
  child_table: string
  child_key: string
  parent_key: string
  fields: FormFieldSpec[]
}

interface FormSpec {
  id: string
  title: string
  table: string
  archetype: string // columnar | continuous | datasheet | master_detail
  on_submit: string
  fields: FormFieldSpec[]
  subform?: SubformSpec | null
  events?: FormEvents | null
}

// FormEvents binds form/record lifecycle events to macro ids (P5.7).
interface FormEvents {
  on_open?: string
  on_load?: string
  before_insert?: string
  after_insert?: string
  before_update?: string
  after_update?: string
}

// MacroRunResult / MacroIntent mirror studio.MacroRunResult — the ordered UI
// intent list RunMacro / RunAutoExec / FireFormOpen return for the frontend.
interface MacroRunResult {
  intents: { kind: string; target: string; args?: Record<string, string> }[]
}

interface FormSummary {
  id: string
  title: string
  archetype: string
}

// FormSubmitRow mirrors studio.FormSubmitRow — one row in an atomic submit.
interface FormSubmitRow {
  table: string
  values: Record<string, any>
}

// AppConfig mirrors studio.AppConfig (Phase 4 dual-mode). `mode` is the live
// runtime mode ("studio" | "app"); default_mode / startup_object / app_title are
// persisted in _vibe_meta.
interface AppConfig {
  mode: string
  default_mode: string
  startup_object: string
  app_title: string
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
}

interface PageSummary {
  id: string
  title: string
  page_type: string
  sort_order: number
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
