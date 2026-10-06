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

export namespace hub {
	
	export class ClaudeConfig {
	    name: string;
	    desktop: string;
	    code: string;
	
	    static createFrom(source: any = {}) {
	        return new ClaudeConfig(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.desktop = source["desktop"];
	        this.code = source["code"];
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
	    teamSandbox: string;
	    teamAskBeforeWrites: boolean;
	
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
	        this.teamSandbox = source["teamSandbox"];
	        this.teamAskBeforeWrites = source["teamAskBeforeWrites"];
	    }
	}
	export class AgentPolicyInfo {
	    sandbox: string;
	    askBeforeWrites: boolean;
	
	    static createFrom(source: any = {}) {
	        return new AgentPolicyInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
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
	export class CatalogTable {
	    name: string;
	    qualified: string;
	    kind: string;
	
	    static createFrom(source: any = {}) {
	        return new CatalogTable(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.qualified = source["qualified"];
	        this.kind = source["kind"];
	    }
	}
	export class CatalogGroup {
	    connection: string;
	    type: string;
	    status: string;
	    detail?: string;
	    readOnly: boolean;
	    default: boolean;
	    tables: CatalogTable[];
	
	    static createFrom(source: any = {}) {
	        return new CatalogGroup(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.connection = source["connection"];
	        this.type = source["type"];
	        this.status = source["status"];
	        this.detail = source["detail"];
	        this.readOnly = source["readOnly"];
	        this.default = source["default"];
	        this.tables = this.convertValues(source["tables"], CatalogTable);
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
	export class ConflictRequest {
	    kind: string;
	    id: string;
	    action: string;
	    data: Record<string, any>;
	    rev: string;
	
	    static createFrom(source: any = {}) {
	        return new ConflictRequest(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.kind = source["kind"];
	        this.id = source["id"];
	        this.action = source["action"];
	        this.data = source["data"];
	        this.rev = source["rev"];
	    }
	}
	export class ConflictResult {
	    id: string;
	    rev: string;
	    title: string;
	
	    static createFrom(source: any = {}) {
	        return new ConflictResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.rev = source["rev"];
	        this.title = source["title"];
	    }
	}
	export class ConnectionInfo {
	    name: string;
	    type: string;
	    scope: string;
	    shadows?: string[];
	    readOnly: boolean;
	    default: boolean;
	    status: string;
	    detail?: string;
	    lockedByPid?: number;
	
	    static createFrom(source: any = {}) {
	        return new ConnectionInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.type = source["type"];
	        this.scope = source["scope"];
	        this.shadows = source["shadows"];
	        this.readOnly = source["readOnly"];
	        this.default = source["default"];
	        this.status = source["status"];
	        this.detail = source["detail"];
	        this.lockedByPid = source["lockedByPid"];
	    }
	}
	export class CreateWorkspaceRequest {
	    name: string;
	    dir: string;
	    parentDir: string;
	    git: boolean;
	    template: string;
	    inPlace: boolean;
	
	    static createFrom(source: any = {}) {
	        return new CreateWorkspaceRequest(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.dir = source["dir"];
	        this.parentDir = source["parentDir"];
	        this.git = source["git"];
	        this.template = source["template"];
	        this.inPlace = source["inPlace"];
	    }
	}
	export class DashboardData {
	    title: string;
	    description: string;
	    notebook_count: number;
	    table_count: number;
	    query_count: number;
	    skill_count: number;
	    local_tables: string[];
	    default_connection: string;
	    read_only: boolean;
	
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
	        this.skill_count = source["skill_count"];
	        this.local_tables = source["local_tables"];
	        this.default_connection = source["default_connection"];
	        this.read_only = source["read_only"];
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
	export class HolderInfo {
	    app: string;
	    pid: number;
	    servesMcp: boolean;
	    text: string;
	
	    static createFrom(source: any = {}) {
	        return new HolderInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.app = source["app"];
	        this.pid = source["pid"];
	        this.servesMcp = source["servesMcp"];
	        this.text = source["text"];
	    }
	}
	export class InitialWorkspace {
	    path: string;
	    message: string;
	
	    static createFrom(source: any = {}) {
	        return new InitialWorkspace(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.path = source["path"];
	        this.message = source["message"];
	    }
	}
	export class LLMKeyStatus {
	    provider: string;
	    source: string;
	    set: boolean;
	    fileKey: boolean;
	    canMigrate: boolean;
	    keychainAvailable: boolean;
	    detail?: string;
	
	    static createFrom(source: any = {}) {
	        return new LLMKeyStatus(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.provider = source["provider"];
	        this.source = source["source"];
	        this.set = source["set"];
	        this.fileKey = source["fileKey"];
	        this.canMigrate = source["canMigrate"];
	        this.keychainAvailable = source["keychainAvailable"];
	        this.detail = source["detail"];
	    }
	}
	export class MCPStatus {
	    running: boolean;
	    url: string;
	    canStart: boolean;
	    reason: string;
	
	    static createFrom(source: any = {}) {
	        return new MCPStatus(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.running = source["running"];
	        this.url = source["url"];
	        this.canStart = source["canStart"];
	        this.reason = source["reason"];
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
	export class WorkspaceCounts {
	    notebooks: number;
	    queries: number;
	    skills: number;
	    tables: number;
	
	    static createFrom(source: any = {}) {
	        return new WorkspaceCounts(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.notebooks = source["notebooks"];
	        this.queries = source["queries"];
	        this.skills = source["skills"];
	        this.tables = source["tables"];
	    }
	}
	export class WorkspaceInfo {
	    root: string;
	    id: string;
	    name: string;
	    sessionId: string;
	    gitBranch: string;
	    isGit: boolean;
	    defaultConnection: string;
	    connections: ConnectionInfo[];
	    counts: WorkspaceCounts;
	    policy: AgentPolicyInfo;
	    team: AgentPolicyInfo;
	    local: AgentPolicyInfo;
	    teamSandboxSet: boolean;
	    readOnly: boolean;
	    holder?: HolderInfo;
	    issues: workspace.SessionIssue[];
	    warnings: string[];
	    duplicateIds: string[];
	    localUntrusted: boolean;
	    mcpRunning: boolean;
	
	    static createFrom(source: any = {}) {
	        return new WorkspaceInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.root = source["root"];
	        this.id = source["id"];
	        this.name = source["name"];
	        this.sessionId = source["sessionId"];
	        this.gitBranch = source["gitBranch"];
	        this.isGit = source["isGit"];
	        this.defaultConnection = source["defaultConnection"];
	        this.connections = this.convertValues(source["connections"], ConnectionInfo);
	        this.counts = this.convertValues(source["counts"], WorkspaceCounts);
	        this.policy = this.convertValues(source["policy"], AgentPolicyInfo);
	        this.team = this.convertValues(source["team"], AgentPolicyInfo);
	        this.local = this.convertValues(source["local"], AgentPolicyInfo);
	        this.teamSandboxSet = source["teamSandboxSet"];
	        this.readOnly = source["readOnly"];
	        this.holder = this.convertValues(source["holder"], HolderInfo);
	        this.issues = this.convertValues(source["issues"], workspace.SessionIssue);
	        this.warnings = source["warnings"];
	        this.duplicateIds = source["duplicateIds"];
	        this.localUntrusted = source["localUntrusted"];
	        this.mcpRunning = source["mcpRunning"];
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
	export class OpenWorkspaceResult {
	    status: string;
	    path: string;
	    info?: WorkspaceInfo;
	    warnings?: string[];
	
	    static createFrom(source: any = {}) {
	        return new OpenWorkspaceResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.status = source["status"];
	        this.path = source["path"];
	        this.info = this.convertValues(source["info"], WorkspaceInfo);
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
	export class QuerySaveResult {
	    id: string;
	    rev: string;
	
	    static createFrom(source: any = {}) {
	        return new QuerySaveResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.rev = source["rev"];
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
	export class RecentWorkspace {
	    path: string;
	    id: string;
	    name: string;
	    lastOpened: string;
	    gitBranch: string;
	    missing: boolean;
	
	    static createFrom(source: any = {}) {
	        return new RecentWorkspace(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.path = source["path"];
	        this.id = source["id"];
	        this.name = source["name"];
	        this.lastOpened = source["lastOpened"];
	        this.gitBranch = source["gitBranch"];
	        this.missing = source["missing"];
	    }
	}
	export class SkillSummary {
	    name: string;
	    description: string;
	    instructions: string;
	    table_name?: string;
	    source: string;
	    rev?: string;
	
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
	        this.rev = source["rev"];
	    }
	}
	export class SwitchRequest {
	    path: string;
	
	    static createFrom(source: any = {}) {
	        return new SwitchRequest(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.path = source["path"];
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
	
	export class WorkspaceDirCheck {
	    dir: string;
	    exists: boolean;
	    notEmpty: boolean;
	
	    static createFrom(source: any = {}) {
	        return new WorkspaceDirCheck(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.dir = source["dir"];
	        this.exists = source["exists"];
	        this.notEmpty = source["notEmpty"];
	    }
	}
	
	export class WorkspaceSettings {
	    name: string;
	    defaultConnection: string;
	    teamSandbox: string;
	    teamAskBeforeChanges: boolean;
	    sandbox: string;
	    askBeforeChanges: boolean;
	    loadedTeamSandbox?: string;
	    loadedTeamAsk?: boolean;
	
	    static createFrom(source: any = {}) {
	        return new WorkspaceSettings(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.defaultConnection = source["defaultConnection"];
	        this.teamSandbox = source["teamSandbox"];
	        this.teamAskBeforeChanges = source["teamAskBeforeChanges"];
	        this.sandbox = source["sandbox"];
	        this.askBeforeChanges = source["askBeforeChanges"];
	        this.loadedTeamSandbox = source["loadedTeamSandbox"];
	        this.loadedTeamAsk = source["loadedTeamAsk"];
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
	    Connection: string;
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
	        this.Connection = source["Connection"];
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
	    rev?: string;
	
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
	        this.rev = source["rev"];
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
	    error?: string;
	
	    static createFrom(source: any = {}) {
	        return new Summary(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.title = source["title"];
	        this.cellCount = source["cellCount"];
	        this.updatedAt = source["updatedAt"];
	        this.error = source["error"];
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
	    description?: string;
	    connection?: string;
	    rev?: string;
	    error?: string;
	
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
	        this.description = source["description"];
	        this.connection = source["connection"];
	        this.rev = source["rev"];
	        this.error = source["error"];
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

export namespace secrets {
	
	export class Probe {
	    available: boolean;
	    backend: string;
	    detail?: string;
	
	    static createFrom(source: any = {}) {
	        return new Probe(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.available = source["available"];
	        this.backend = source["backend"];
	        this.detail = source["detail"];
	    }
	}

}

export namespace session {
	
	export class Status {
	    name: string;
	    type: string;
	    status: string;
	    message?: string;
	    locked_by_pid?: number;
	    read_only: boolean;
	    default?: boolean;
	
	    static createFrom(source: any = {}) {
	        return new Status(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.type = source["type"];
	        this.status = source["status"];
	        this.message = source["message"];
	        this.locked_by_pid = source["locked_by_pid"];
	        this.read_only = source["read_only"];
	        this.default = source["default"];
	    }
	}

}

export namespace workspace {
	
	export class SessionIssue {
	    connection: string;
	    message: string;
	
	    static createFrom(source: any = {}) {
	        return new SessionIssue(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.connection = source["connection"];
	        this.message = source["message"];
	    }
	}

}

