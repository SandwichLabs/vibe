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
          CloseFile(): Promise<void>
          SaveFile(): Promise<void>
          IsFileOpen(): Promise<boolean>
          GetFileInfo(): Promise<FileInfo>
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
          InsertRow(table: string, values: Record<string, any>): Promise<WriteResult>
          UpdateRow(table: string, pkCol: string, pkVal: any, values: Record<string, any>): Promise<WriteResult>
          DeleteRow(table: string, pkCol: string, pkVal: any): Promise<WriteResult>
          CreateTable(name: string, columns: ColumnInfo[]): Promise<void>
          DropTable(name: string): Promise<void>
          ExecuteQuery(sql: string, engine: string): Promise<QueryResult>
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
