export namespace a2ui {
	
	export class Component {
	    ID: string;
	    Type: string;
	    Parent: string;
	    Fields: Record<string, any>;
	
	    static createFrom(source: any = {}) {
	        return new Component(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.ID = source["ID"];
	        this.Type = source["Type"];
	        this.Parent = source["Parent"];
	        this.Fields = source["Fields"];
	    }
	}
	export class Page {
	    Title: string;
	    Components: Component[];
	
	    static createFrom(source: any = {}) {
	        return new Page(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.Title = source["Title"];
	        this.Components = this.convertValues(source["Components"], Component);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}

}

export namespace crud {
	
	export class WriteResult {
	    affected: number;
	
	    static createFrom(source: any = {}) {
	        return new WriteResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.affected = source["affected"];
	    }
	}

}

export namespace importer {
	
	export class BadValue {
	    row: number;
	    column: string;
	    value: string;
	    want: string;
	    reason: string;
	
	    static createFrom(source: any = {}) {
	        return new BadValue(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.row = source["row"];
	        this.column = source["column"];
	        this.value = source["value"];
	        this.want = source["want"];
	        this.reason = source["reason"];
	    }
	}
	export class ColType {
	    type: string;
	    scale?: number;
	
	    static createFrom(source: any = {}) {
	        return new ColType(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.type = source["type"];
	        this.scale = source["scale"];
	    }
	}
	export class Column {
	    name: string;
	    source_name: string;
	    inferred: ColType;
	    candidate: boolean;
	
	    static createFrom(source: any = {}) {
	        return new Column(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.source_name = source["source_name"];
	        this.inferred = this.convertValues(source["inferred"], ColType);
	        this.candidate = source["candidate"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class Inspection {
	    sheets?: string[];
	    sheet?: string;
	    columns: Column[];
	    rows: string[][];
	    row_count: number;
	    suggested_key: string;
	
	    static createFrom(source: any = {}) {
	        return new Inspection(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.sheets = source["sheets"];
	        this.sheet = source["sheet"];
	        this.columns = this.convertValues(source["columns"], Column);
	        this.rows = source["rows"];
	        this.row_count = source["row_count"];
	        this.suggested_key = source["suggested_key"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class Options {
	    path: string;
	    sheet: string;
	    has_header: boolean;
	    table: string;
	    mode: string;
	    key: string;
	    types: Record<string, ColType>;
	    dry_run: boolean;
	
	    static createFrom(source: any = {}) {
	        return new Options(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.path = source["path"];
	        this.sheet = source["sheet"];
	        this.has_header = source["has_header"];
	        this.table = source["table"];
	        this.mode = source["mode"];
	        this.key = source["key"];
	        this.types = this.convertValues(source["types"], ColType, true);
	        this.dry_run = source["dry_run"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class Source {
	    path: string;
	    sheet: string;
	    has_header: boolean;
	
	    static createFrom(source: any = {}) {
	        return new Source(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.path = source["path"];
	        this.sheet = source["sheet"];
	        this.has_header = source["has_header"];
	    }
	}
	export class Summary {
	    ok: boolean;
	    mode: string;
	    table: string;
	    exists: boolean;
	    existing_rows: number;
	    table_key: string;
	    modes: string[];
	    rows: number;
	    inserted: number;
	    updated: number;
	    unchanged: number;
	    replaced: number;
	    exact_dupes: number;
	    missing: string[];
	    extra: string[];
	    bad_values: BadValue[];
	    bad_count: number;
	    blocked: string;
	
	    static createFrom(source: any = {}) {
	        return new Summary(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.ok = source["ok"];
	        this.mode = source["mode"];
	        this.table = source["table"];
	        this.exists = source["exists"];
	        this.existing_rows = source["existing_rows"];
	        this.table_key = source["table_key"];
	        this.modes = source["modes"];
	        this.rows = source["rows"];
	        this.inserted = source["inserted"];
	        this.updated = source["updated"];
	        this.unchanged = source["unchanged"];
	        this.replaced = source["replaced"];
	        this.exact_dupes = source["exact_dupes"];
	        this.missing = source["missing"];
	        this.extra = source["extra"];
	        this.bad_values = this.convertValues(source["bad_values"], BadValue);
	        this.bad_count = source["bad_count"];
	        this.blocked = source["blocked"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}

}

export namespace llm {
	
	export class EnvelopeCell {
	    type: string;
	    sql?: string;
	    view?: string;
	    label?: string;
	    markdown?: string;
	
	    static createFrom(source: any = {}) {
	        return new EnvelopeCell(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.type = source["type"];
	        this.sql = source["sql"];
	        this.view = source["view"];
	        this.label = source["label"];
	        this.markdown = source["markdown"];
	    }
	}
	export class TaskEnvelope {
	    outcome: string;
	    cell?: EnvelopeCell;
	    explanation?: string;
	    checked?: string[];
	    questions?: string[];
	    missingData?: string[];
	    notes?: string;
	
	    static createFrom(source: any = {}) {
	        return new TaskEnvelope(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.outcome = source["outcome"];
	        this.cell = this.convertValues(source["cell"], EnvelopeCell);
	        this.explanation = source["explanation"];
	        this.checked = source["checked"];
	        this.questions = source["questions"];
	        this.missingData = source["missingData"];
	        this.notes = source["notes"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}

}

export namespace main {
	
	export class A2UIQueryResult {
	    columns: string[];
	    types: string[];
	    rows: any[][];
	    truncated: boolean;
	
	    static createFrom(source: any = {}) {
	        return new A2UIQueryResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.columns = source["columns"];
	        this.types = source["types"];
	        this.rows = source["rows"];
	        this.truncated = source["truncated"];
	    }
	}
	export class AgentToolStep {
	    tool: string;
	    input: string;
	    output: string;
	    isError?: boolean;
	
	    static createFrom(source: any = {}) {
	        return new AgentToolStep(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.tool = source["tool"];
	        this.input = source["input"];
	        this.output = source["output"];
	        this.isError = source["isError"];
	    }
	}
	export class AgentChatResult {
	    text: string;
	    steps: AgentToolStep[];
	
	    static createFrom(source: any = {}) {
	        return new AgentChatResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.text = source["text"];
	        this.steps = this.convertValues(source["steps"], AgentToolStep);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class AgentConfig {
	    provider: string;
	    model: string;
	    endpoint: string;
	    sandbox: string;
	    askBeforeWrites: boolean;
	
	    static createFrom(source: any = {}) {
	        return new AgentConfig(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.provider = source["provider"];
	        this.model = source["model"];
	        this.endpoint = source["endpoint"];
	        this.sandbox = source["sandbox"];
	        this.askBeforeWrites = source["askBeforeWrites"];
	    }
	}
	export class AgentResult {
	    sql?: string;
	    columns?: string[];
	    rows?: any[][];
	    analysis?: string;
	    error?: string;
	
	    static createFrom(source: any = {}) {
	        return new AgentResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.sql = source["sql"];
	        this.columns = source["columns"];
	        this.rows = source["rows"];
	        this.analysis = source["analysis"];
	        this.error = source["error"];
	    }
	}
	export class TaskTurnArg {
	    role: string;
	    text: string;
	
	    static createFrom(source: any = {}) {
	        return new TaskTurnArg(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.role = source["role"];
	        this.text = source["text"];
	    }
	}
	export class TaskCellArg {
	    type: string;
	    sql?: string;
	    view?: string;
	    label?: string;
	    source?: string;
	
	    static createFrom(source: any = {}) {
	        return new TaskCellArg(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.type = source["type"];
	        this.sql = source["sql"];
	        this.view = source["view"];
	        this.label = source["label"];
	        this.source = source["source"];
	    }
	}
	export class AgentTaskRequest {
	    taskId: string;
	    mode: string;
	    description: string;
	    cell?: TaskCellArg;
	    tables?: string[];
	    notebookSummary?: string[];
	    followUp?: TaskTurnArg[];
	
	    static createFrom(source: any = {}) {
	        return new AgentTaskRequest(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.taskId = source["taskId"];
	        this.mode = source["mode"];
	        this.description = source["description"];
	        this.cell = this.convertValues(source["cell"], TaskCellArg);
	        this.tables = source["tables"];
	        this.notebookSummary = source["notebookSummary"];
	        this.followUp = this.convertValues(source["followUp"], TaskTurnArg);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	
	export class AgentTurn {
	    role: string;
	    text: string;
	
	    static createFrom(source: any = {}) {
	        return new AgentTurn(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.role = source["role"];
	        this.text = source["text"];
	    }
	}
	export class ColumnInfo {
	    name: string;
	    type: string;
	    not_null: boolean;
	    pk: boolean;
	    fk: boolean;
	
	    static createFrom(source: any = {}) {
	        return new ColumnInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.type = source["type"];
	        this.not_null = source["not_null"];
	        this.pk = source["pk"];
	        this.fk = source["fk"];
	    }
	}
	export class DashboardData {
	    title: string;
	    description: string;
	    notebook_count: number;
	    table_count: number;
	    query_count: number;
	    file_size: number;
	    created_at: string;
	    updated_at: string;
	
	    static createFrom(source: any = {}) {
	        return new DashboardData(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.title = source["title"];
	        this.description = source["description"];
	        this.notebook_count = source["notebook_count"];
	        this.table_count = source["table_count"];
	        this.query_count = source["query_count"];
	        this.file_size = source["file_size"];
	        this.created_at = source["created_at"];
	        this.updated_at = source["updated_at"];
	    }
	}
	export class FileInfo {
	    path: string;
	    title: string;
	    description: string;
	    notebook_count: number;
	    table_count: number;
	    query_count: number;
	    file_size: number;
	    session_id: string;
	
	    static createFrom(source: any = {}) {
	        return new FileInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.path = source["path"];
	        this.title = source["title"];
	        this.description = source["description"];
	        this.notebook_count = source["notebook_count"];
	        this.table_count = source["table_count"];
	        this.query_count = source["query_count"];
	        this.file_size = source["file_size"];
	        this.session_id = source["session_id"];
	    }
	}
	export class FilterCondition {
	    column: string;
	    op: string;
	    value: any;
	
	    static createFrom(source: any = {}) {
	        return new FilterCondition(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.column = source["column"];
	        this.op = source["op"];
	        this.value = source["value"];
	    }
	}
	export class FilterSpec {
	    combinator: string;
	    conditions: FilterCondition[];
	
	    static createFrom(source: any = {}) {
	        return new FilterSpec(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.combinator = source["combinator"];
	        this.conditions = this.convertValues(source["conditions"], FilterCondition);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class NotebookSaveResult {
	    notebook: notebooks.Notebook;
	    warnings: string[];
	
	    static createFrom(source: any = {}) {
	        return new NotebookSaveResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.notebook = this.convertValues(source["notebook"], notebooks.Notebook);
	        this.warnings = source["warnings"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class QueryResult {
	    columns: ColumnInfo[];
	    rows: any[][];
	    duration: string;
	    row_count: number;
	    total_rows: number;
	
	    static createFrom(source: any = {}) {
	        return new QueryResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.columns = this.convertValues(source["columns"], ColumnInfo);
	        this.rows = source["rows"];
	        this.duration = source["duration"];
	        this.row_count = source["row_count"];
	        this.total_rows = source["total_rows"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class QuerySummary {
	    id: string;
	    title: string;
	    engine: string;
	
	    static createFrom(source: any = {}) {
	        return new QuerySummary(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.title = source["title"];
	        this.engine = source["engine"];
	    }
	}
	export class SkillSummary {
	    name: string;
	    description: string;
	    instructions: string;
	    table_name?: string;
	    source: string;
	
	    static createFrom(source: any = {}) {
	        return new SkillSummary(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.description = source["description"];
	        this.instructions = source["instructions"];
	        this.table_name = source["table_name"];
	        this.source = source["source"];
	    }
	}
	export class TableData {
	    columns: string[];
	    types: string[];
	    rows: any[][];
	    page: number;
	    page_size: number;
	    total_rows: number;
	    total_pages: number;
	
	    static createFrom(source: any = {}) {
	        return new TableData(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.columns = source["columns"];
	        this.types = source["types"];
	        this.rows = source["rows"];
	        this.page = source["page"];
	        this.page_size = source["page_size"];
	        this.total_rows = source["total_rows"];
	        this.total_pages = source["total_pages"];
	    }
	}
	export class TableInfo {
	    name: string;
	    columns: ColumnInfo[];
	
	    static createFrom(source: any = {}) {
	        return new TableInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.columns = this.convertValues(source["columns"], ColumnInfo);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class TableStat {
	    name: string;
	    rows: number;
	
	    static createFrom(source: any = {}) {
	        return new TableStat(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.rows = source["rows"];
	    }
	}
	
	
	export class TemplateInfo {
	    name: string;
	    description: string;
	
	    static createFrom(source: any = {}) {
	        return new TemplateInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.description = source["description"];
	    }
	}
	export class UserThemesResult {
	    themes: prefs.ThemeFile[];
	    problems: string[];
	    dir: string;
	
	    static createFrom(source: any = {}) {
	        return new UserThemesResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.themes = this.convertValues(source["themes"], prefs.ThemeFile);
	        this.problems = source["problems"];
	        this.dir = source["dir"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}

}

export namespace notebooks {
	
	export class Cell {
	    ID: string;
	    Type: string;
	    Collapsed: boolean;
	    Source: string;
	    View: string;
	    ViewPinned: boolean;
	    Label: string;
	    Page?: a2ui.Page;
	
	    static createFrom(source: any = {}) {
	        return new Cell(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.ID = source["ID"];
	        this.Type = source["Type"];
	        this.Collapsed = source["Collapsed"];
	        this.Source = source["Source"];
	        this.View = source["View"];
	        this.ViewPinned = source["ViewPinned"];
	        this.Label = source["Label"];
	        this.Page = this.convertValues(source["Page"], a2ui.Page);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class Notebook {
	    schemaVersion: number;
	    id: string;
	    title: string;
	    cells: Cell[];
	    updatedAt: string;
	
	    static createFrom(source: any = {}) {
	        return new Notebook(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.schemaVersion = source["schemaVersion"];
	        this.id = source["id"];
	        this.title = source["title"];
	        this.cells = this.convertValues(source["cells"], Cell);
	        this.updatedAt = source["updatedAt"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class Summary {
	    id: string;
	    title: string;
	    cellCount: number;
	    updatedAt: string;
	
	    static createFrom(source: any = {}) {
	        return new Summary(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.title = source["title"];
	        this.cellCount = source["cellCount"];
	        this.updatedAt = source["updatedAt"];
	    }
	}

}

export namespace prefs {
	
	export class Preferences {
	    theme: string;
	    systemLight: string;
	    systemDark: string;
	    reduceMotion: boolean;
	    uiScale: number;
	
	    static createFrom(source: any = {}) {
	        return new Preferences(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.theme = source["theme"];
	        this.systemLight = source["systemLight"];
	        this.systemDark = source["systemDark"];
	        this.reduceMotion = source["reduceMotion"];
	        this.uiScale = source["uiScale"];
	    }
	}
	export class ThemeFile {
	    path: string;
	    filename: string;
	    json: string;
	
	    static createFrom(source: any = {}) {
	        return new ThemeFile(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.path = source["path"];
	        this.filename = source["filename"];
	        this.json = source["json"];
	    }
	}

}

export namespace queries {
	
	export class Param {
	    name: string;
	    type: string;
	    required: boolean;
	    default?: string;
	
	    static createFrom(source: any = {}) {
	        return new Param(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.type = source["type"];
	        this.required = source["required"];
	        this.default = source["default"];
	    }
	}
	export class Query {
	    id: string;
	    title: string;
	    sql: string;
	    engine: string;
	    parameters: Param[];
	    created_at: string;
	    updated_at: string;
	
	    static createFrom(source: any = {}) {
	        return new Query(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.title = source["title"];
	        this.sql = source["sql"];
	        this.engine = source["engine"];
	        this.parameters = this.convertValues(source["parameters"], Param);
	        this.created_at = source["created_at"];
	        this.updated_at = source["updated_at"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}

}

