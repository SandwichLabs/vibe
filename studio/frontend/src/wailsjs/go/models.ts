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

export namespace bundle {
	
	export class Outcome {
	    key: string;
	    kind: string;
	    id: string;
	    finalId?: string;
	    result: string;
	    error?: string;
	
	    static createFrom(source: any = {}) {
	        return new Outcome(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.key = source["key"];
	        this.kind = source["kind"];
	        this.id = source["id"];
	        this.finalId = source["finalId"];
	        this.result = source["result"];
	        this.error = source["error"];
	    }
	}
	export class Result {
	    outcomes: Outcome[];
	    dryRun: boolean;
	    warnings: string[];
	
	    static createFrom(source: any = {}) {
	        return new Result(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.outcomes = this.convertValues(source["outcomes"], Outcome);
	        this.dryRun = source["dryRun"];
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
	export class Selection {
	    all: boolean;
	    notebooks: string[];
	    queries: string[];
	    skills: string[];
	    sharedConnections: boolean;
	    teamConnections: string[];
	    localConnections: string[];
	
	    static createFrom(source: any = {}) {
	        return new Selection(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.all = source["all"];
	        this.notebooks = source["notebooks"];
	        this.queries = source["queries"];
	        this.skills = source["skills"];
	        this.sharedConnections = source["sharedConnections"];
	        this.teamConnections = source["teamConnections"];
	        this.localConnections = source["localConnections"];
	    }
	}

}

export namespace config {
	
	export class LLMSettings {
	    provider: string;
	    model: string;
	    endpoint: string;
	    accountId: string;
	    gateway: string;
	
	    static createFrom(source: any = {}) {
	        return new LLMSettings(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.provider = source["provider"];
	        this.model = source["model"];
	        this.endpoint = source["endpoint"];
	        this.accountId = source["accountId"];
	        this.gateway = source["gateway"];
	    }
	}
	export class ProviderInfo {
	    id: string;
	    label: string;
	    description: string;
	    needsKey: boolean;
	    keyOptional: boolean;
	    keyLabel: string;
	    keyEnvVars: string[];
	    hasEndpoint: boolean;
	    defaultEndpoint?: string;
	    hasAccount: boolean;
	    defaultModel: string;
	    suggestedModels: string[];
	
	    static createFrom(source: any = {}) {
	        return new ProviderInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.label = source["label"];
	        this.description = source["description"];
	        this.needsKey = source["needsKey"];
	        this.keyOptional = source["keyOptional"];
	        this.keyLabel = source["keyLabel"];
	        this.keyEnvVars = source["keyEnvVars"];
	        this.hasEndpoint = source["hasEndpoint"];
	        this.defaultEndpoint = source["defaultEndpoint"];
	        this.hasAccount = source["hasAccount"];
	        this.defaultModel = source["defaultModel"];
	        this.suggestedModels = source["suggestedModels"];
	    }
	}

}

export namespace connections {
	
	export class Capabilities {
	    catalog: boolean;
	    writable: boolean;
	    readOnlyAttach: boolean;
	    remote: boolean;
	    test: boolean;
	    subprocess?: boolean;
	
	    static createFrom(source: any = {}) {
	        return new Capabilities(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.catalog = source["catalog"];
	        this.writable = source["writable"];
	        this.readOnlyAttach = source["readOnlyAttach"];
	        this.remote = source["remote"];
	        this.test = source["test"];
	        this.subprocess = source["subprocess"];
	    }
	}
	export class Field {
	    name: string;
	    label: string;
	    kind: string;
	    help?: string;
	    default?: string;
	    choices?: string[];
	    required: boolean;
	    secret: boolean;
	    perPerson: boolean;
	    option?: boolean;
	
	    static createFrom(source: any = {}) {
	        return new Field(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.label = source["label"];
	        this.kind = source["kind"];
	        this.help = source["help"];
	        this.default = source["default"];
	        this.choices = source["choices"];
	        this.required = source["required"];
	        this.secret = source["secret"];
	        this.perPerson = source["perPerson"];
	        this.option = source["option"];
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
	    connection?: string;
	
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
	        this.connection = source["connection"];
	    }
	}
	export class ModelList {
	    models: string[];
	    error?: string;
	    unreachable?: boolean;
	
	    static createFrom(source: any = {}) {
	        return new ModelList(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.models = source["models"];
	        this.error = source["error"];
	        this.unreachable = source["unreachable"];
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
	    connection?: string;
	
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
	        this.connection = source["connection"];
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
	export class BundleContentConnection {
	    name: string;
	    type: string;
	    summary: string;
	    scope: string;
	    portable: boolean;
	    reason?: string;
	
	    static createFrom(source: any = {}) {
	        return new BundleContentConnection(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.type = source["type"];
	        this.summary = source["summary"];
	        this.scope = source["scope"];
	        this.portable = source["portable"];
	        this.reason = source["reason"];
	    }
	}
	export class BundleContentItem {
	    id: string;
	    title: string;
	
	    static createFrom(source: any = {}) {
	        return new BundleContentItem(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.title = source["title"];
	    }
	}
	export class BundleContent {
	    workspaceName: string;
	    notebooks: BundleContentItem[];
	    queries: BundleContentItem[];
	    skills: BundleContentItem[];
	    connections: BundleContentConnection[];
	
	    static createFrom(source: any = {}) {
	        return new BundleContent(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.workspaceName = source["workspaceName"];
	        this.notebooks = this.convertValues(source["notebooks"], BundleContentItem);
	        this.queries = this.convertValues(source["queries"], BundleContentItem);
	        this.skills = this.convertValues(source["skills"], BundleContentItem);
	        this.connections = this.convertValues(source["connections"], BundleContentConnection);
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
	
	
	export class BundleDecisions {
	    onConflict: string;
	    items: Record<string, string>;
	    connections: string[];
	
	    static createFrom(source: any = {}) {
	        return new BundleDecisions(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.onConflict = source["onConflict"];
	        this.items = source["items"];
	        this.connections = source["connections"];
	    }
	}
	export class BundleExportRequest {
	    path: string;
	    selection: bundle.Selection;
	    passphrase: string;
	
	    static createFrom(source: any = {}) {
	        return new BundleExportRequest(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.path = source["path"];
	        this.selection = this.convertValues(source["selection"], bundle.Selection);
	        this.passphrase = source["passphrase"];
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
	export class BundleExportResult {
	    path: string;
	    cancelled: boolean;
	    encrypted: boolean;
	    notebooks: number;
	    queries: number;
	    skills: number;
	    connections: number;
	    bytes: number;
	    warnings: string[];
	
	    static createFrom(source: any = {}) {
	        return new BundleExportResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.path = source["path"];
	        this.cancelled = source["cancelled"];
	        this.encrypted = source["encrypted"];
	        this.notebooks = source["notebooks"];
	        this.queries = source["queries"];
	        this.skills = source["skills"];
	        this.connections = source["connections"];
	        this.bytes = source["bytes"];
	        this.warnings = source["warnings"];
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
	export class ServerChange {
	    old: string;
	    new: string;
	
	    static createFrom(source: any = {}) {
	        return new ServerChange(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.old = source["old"];
	        this.new = source["new"];
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
	    remote: boolean;
	    catalogless?: boolean;
	    signIn?: string;
	    extension?: string;
	    serverChanged?: ServerChange;
	
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
	        this.remote = source["remote"];
	        this.catalogless = source["catalogless"];
	        this.signIn = source["signIn"];
	        this.extension = source["extension"];
	        this.serverChanged = this.convertValues(source["serverChanged"], ServerChange);
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
	export class BundleImportOpened {
	    opened?: OpenWorkspaceResult;
	    result?: bundle.Result;
	
	    static createFrom(source: any = {}) {
	        return new BundleImportOpened(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.opened = this.convertValues(source["opened"], OpenWorkspaceResult);
	        this.result = this.convertValues(source["result"], bundle.Result);
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
	export class BundlePreview {
	    handle: string;
	    needsPassphrase: boolean;
	    wrongPassphrase: boolean;
	    path: string;
	    workspaceName: string;
	    createdAt: string;
	    createdBy: string;
	    encrypted: boolean;
	    items: bundle.PreviewItem[];
	    connections: bundle.PreviewConnection[];
	    warnings: string[];
	    makerNotes: string[];
	
	    static createFrom(source: any = {}) {
	        return new BundlePreview(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.handle = source["handle"];
	        this.needsPassphrase = source["needsPassphrase"];
	        this.wrongPassphrase = source["wrongPassphrase"];
	        this.path = source["path"];
	        this.workspaceName = source["workspaceName"];
	        this.createdAt = source["createdAt"];
	        this.createdBy = source["createdBy"];
	        this.encrypted = source["encrypted"];
	        this.items = this.convertValues(source["items"], bundle.PreviewItem);
	        this.connections = this.convertValues(source["connections"], bundle.PreviewConnection);
	        this.warnings = source["warnings"];
	        this.makerNotes = source["makerNotes"];
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
	export class CatalogNode {
	    kind: string;
	    name: string;
	    qualified?: string;
	    rows: number;
	    dataType?: string;
	    pk?: boolean;
	    fk?: boolean;
	
	    static createFrom(source: any = {}) {
	        return new CatalogNode(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.kind = source["kind"];
	        this.name = source["name"];
	        this.qualified = source["qualified"];
	        this.rows = source["rows"];
	        this.dataType = source["dataType"];
	        this.pk = source["pk"];
	        this.fk = source["fk"];
	    }
	}
	export class CatalogListing {
	    kind: string;
	    schema: string;
	    flattened: boolean;
	    nodes: CatalogNode[];
	
	    static createFrom(source: any = {}) {
	        return new CatalogListing(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.kind = source["kind"];
	        this.schema = source["schema"];
	        this.flattened = source["flattened"];
	        this.nodes = this.convertValues(source["nodes"], CatalogNode);
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
	
	export class CatalogRequest {
	    connection: string;
	    schema: string;
	    table: string;
	
	    static createFrom(source: any = {}) {
	        return new CatalogRequest(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.connection = source["connection"];
	        this.schema = source["schema"];
	        this.table = source["table"];
	    }
	}
	
	export class ClearCredentialsRequest {
	    name: string;
	    field: string;
	
	    static createFrom(source: any = {}) {
	        return new ClearCredentialsRequest(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.field = source["field"];
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
	    connection?: string;
	
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
	        this.connection = source["connection"];
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
	export class ConnectWithCredentialsRequest {
	    name: string;
	    username: string;
	    keyId: string;
	    password: string;
	    token: string;
	    secret: string;
	    remember: boolean;
	
	    static createFrom(source: any = {}) {
	        return new ConnectWithCredentialsRequest(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.username = source["username"];
	        this.keyId = source["keyId"];
	        this.password = source["password"];
	        this.token = source["token"];
	        this.secret = source["secret"];
	        this.remember = source["remember"];
	    }
	}
	export class ConnectionDetail {
	    name: string;
	    scope: string;
	    def: workspace.ConnectionDef;
	    shadows: string[];
	    shadowedBy?: string;
	    signIn: string;
	    signInHint?: string;
	    credential: secrets.CredentialStatus;
	    info: ConnectionInfo;
	
	    static createFrom(source: any = {}) {
	        return new ConnectionDetail(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.scope = source["scope"];
	        this.def = this.convertValues(source["def"], workspace.ConnectionDef);
	        this.shadows = source["shadows"];
	        this.shadowedBy = source["shadowedBy"];
	        this.signIn = source["signIn"];
	        this.signInHint = source["signInHint"];
	        this.credential = this.convertValues(source["credential"], secrets.CredentialStatus);
	        this.info = this.convertValues(source["info"], ConnectionInfo);
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
	
	export class ConnectionList {
	    connections: ConnectionInfo[];
	    relaunchRequired: boolean;
	    relaunchMessage?: string;
	
	    static createFrom(source: any = {}) {
	        return new ConnectionList(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.connections = this.convertValues(source["connections"], ConnectionInfo);
	        this.relaunchRequired = source["relaunchRequired"];
	        this.relaunchMessage = source["relaunchMessage"];
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
	export class ConnectionRef {
	    name: string;
	    scope: string;
	
	    static createFrom(source: any = {}) {
	        return new ConnectionRef(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.scope = source["scope"];
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
	export class CredentialInput {
	    username: string;
	    keyId: string;
	    password: string;
	    token: string;
	    secret: string;
	
	    static createFrom(source: any = {}) {
	        return new CredentialInput(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.username = source["username"];
	        this.keyId = source["keyId"];
	        this.password = source["password"];
	        this.token = source["token"];
	        this.secret = source["secret"];
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
	export class ExtensionInfo {
	    name: string;
	    purpose: string;
	    neededBy: string[];
	    installed: boolean;
	
	    static createFrom(source: any = {}) {
	        return new ExtensionInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.purpose = source["purpose"];
	        this.neededBy = source["neededBy"];
	        this.installed = source["installed"];
	    }
	}
	
	export class InitialWorkspace {
	    path: string;
	    message: string;
	    bundlePath: string;
	
	    static createFrom(source: any = {}) {
	        return new InitialWorkspace(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.path = source["path"];
	        this.message = source["message"];
	        this.bundlePath = source["bundlePath"];
	    }
	}
	export class InstallExtensionRequest {
	    name: string;
	
	    static createFrom(source: any = {}) {
	        return new InstallExtensionRequest(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	    }
	}
	export class LLMKeyStatus {
	    provider: string;
	    source: string;
	    set: boolean;
	    fileKey: boolean;
	    canMigrate: boolean;
	    keychainAvailable: boolean;
	    keychainHasKey: boolean;
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
	        this.keychainHasKey = source["keychainHasKey"];
	        this.detail = source["detail"];
	    }
	}
	export class LLMSettingsView {
	    provider: string;
	    model: string;
	    effectiveModel: string;
	    defaultModel: string;
	    endpoint: string;
	    accountId: string;
	    gateway: string;
	    accountIdFromEnv: boolean;
	    envOverrides: Record<string, string>;
	    providers: config.ProviderInfo[];
	
	    static createFrom(source: any = {}) {
	        return new LLMSettingsView(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.provider = source["provider"];
	        this.model = source["model"];
	        this.effectiveModel = source["effectiveModel"];
	        this.defaultModel = source["defaultModel"];
	        this.endpoint = source["endpoint"];
	        this.accountId = source["accountId"];
	        this.gateway = source["gateway"];
	        this.accountIdFromEnv = source["accountIdFromEnv"];
	        this.envOverrides = source["envOverrides"];
	        this.providers = this.convertValues(source["providers"], config.ProviderInfo);
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
	export class MoveConnectionRequest {
	    name: string;
	    to: string;
	
	    static createFrom(source: any = {}) {
	        return new MoveConnectionRequest(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.to = source["to"];
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
	export class RunQueryRequest {
	    sql: string;
	    connection: string;
	    tabId: string;
	
	    static createFrom(source: any = {}) {
	        return new RunQueryRequest(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.sql = source["sql"];
	        this.connection = source["connection"];
	        this.tabId = source["tabId"];
	    }
	}
	export class SaveConnectionRequest {
	    scope: string;
	    def: workspace.ConnectionDef;
	    isNew: boolean;
	    signIn: string;
	    confirmRebind: boolean;
	
	    static createFrom(source: any = {}) {
	        return new SaveConnectionRequest(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.scope = source["scope"];
	        this.def = this.convertValues(source["def"], workspace.ConnectionDef);
	        this.isNew = source["isNew"];
	        this.signIn = source["signIn"];
	        this.confirmRebind = source["confirmRebind"];
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
	export class SaveQueryRequest {
	    id: string;
	    title: string;
	    sql: string;
	    description: string;
	    rev: string;
	    connection: string;
	
	    static createFrom(source: any = {}) {
	        return new SaveQueryRequest(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.title = source["title"];
	        this.sql = source["sql"];
	        this.description = source["description"];
	        this.rev = source["rev"];
	        this.connection = source["connection"];
	    }
	}
	
	export class SetCredentialsRequest {
	    name: string;
	    method: string;
	    username: string;
	    keyId: string;
	    password: string;
	    token: string;
	    secret: string;
	    remember: boolean;
	    confirmRebind: boolean;
	
	    static createFrom(source: any = {}) {
	        return new SetCredentialsRequest(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.method = source["method"];
	        this.username = source["username"];
	        this.keyId = source["keyId"];
	        this.password = source["password"];
	        this.token = source["token"];
	        this.secret = source["secret"];
	        this.remember = source["remember"];
	        this.confirmRebind = source["confirmRebind"];
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
	export class TableCapabilities {
	    connection: string;
	    editable: boolean;
	    reason: string;
	    text: string;
	
	    static createFrom(source: any = {}) {
	        return new TableCapabilities(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.connection = source["connection"];
	        this.editable = source["editable"];
	        this.reason = source["reason"];
	        this.text = source["text"];
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
	export class TestConnectionRequest {
	    scope: string;
	    def: workspace.ConnectionDef;
	    signIn: string;
	    credentials: CredentialInput;
	
	    static createFrom(source: any = {}) {
	        return new TestConnectionRequest(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.scope = source["scope"];
	        this.def = this.convertValues(source["def"], workspace.ConnectionDef);
	        this.signIn = source["signIn"];
	        this.credentials = this.convertValues(source["credentials"], CredentialInput);
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
	export class TestError {
	    code: string;
	    message: string;
	    details: string;
	
	    static createFrom(source: any = {}) {
	        return new TestError(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.code = source["code"];
	        this.message = source["message"];
	        this.details = source["details"];
	    }
	}
	export class TestResultView {
	    ok: boolean;
	    status: string;
	    ms: number;
	    count: number;
	    detail?: string;
	    summary?: string;
	    error?: TestError;
	    installExtension?: string;
	
	    static createFrom(source: any = {}) {
	        return new TestResultView(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.ok = source["ok"];
	        this.status = source["status"];
	        this.ms = source["ms"];
	        this.count = source["count"];
	        this.detail = source["detail"];
	        this.summary = source["summary"];
	        this.error = this.convertValues(source["error"], TestError);
	        this.installExtension = source["installExtension"];
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
	export class TypeSpec {
	    id: string;
	    label: string;
	    description: string;
	    fields: connections.Field[];
	    credentials: connections.Field[];
	    requiresExtension: string[];
	    capabilities: connections.Capabilities;
	    fingerprintFields: string[];
	    signInMethods: string[];
	    soon: boolean;
	
	    static createFrom(source: any = {}) {
	        return new TypeSpec(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.label = source["label"];
	        this.description = source["description"];
	        this.fields = this.convertValues(source["fields"], connections.Field);
	        this.credentials = this.convertValues(source["credentials"], connections.Field);
	        this.requiresExtension = source["requiresExtension"];
	        this.capabilities = this.convertValues(source["capabilities"], connections.Capabilities);
	        this.fingerprintFields = source["fingerprintFields"];
	        this.signInMethods = source["signInMethods"];
	        this.soon = source["soon"];
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
	
	export class EnvVarStatus {
	    name: string;
	    set: boolean;
	
	    static createFrom(source: any = {}) {
	        return new EnvVarStatus(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.set = source["set"];
	    }
	}
	export class CredentialStatus {
	    method: string;
	    username?: string;
	    keyId?: string;
	    secretSet: boolean;
	    sessionOnly?: boolean;
	    fingerprintMatches: boolean;
	    available: boolean;
	    detail?: string;
	    env?: EnvVarStatus[];
	
	    static createFrom(source: any = {}) {
	        return new CredentialStatus(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.method = source["method"];
	        this.username = source["username"];
	        this.keyId = source["keyId"];
	        this.secretSet = source["secretSet"];
	        this.sessionOnly = source["sessionOnly"];
	        this.fingerprintMatches = source["fingerprintMatches"];
	        this.available = source["available"];
	        this.detail = source["detail"];
	        this.env = this.convertValues(source["env"], EnvVarStatus);
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

export namespace workspace {
	
	export class ConnectionDef {
	    name: string;
	    type: string;
	    path?: string;
	    host?: string;
	    port?: number;
	    database?: string;
	    region?: string;
	    scope?: string;
	    read_only?: boolean;
	    credentials?: string;
	    options?: Record<string, string>;
	
	    static createFrom(source: any = {}) {
	        return new ConnectionDef(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.type = source["type"];
	        this.path = source["path"];
	        this.host = source["host"];
	        this.port = source["port"];
	        this.database = source["database"];
	        this.region = source["region"];
	        this.scope = source["scope"];
	        this.read_only = source["read_only"];
	        this.credentials = source["credentials"];
	        this.options = source["options"];
	    }
	}
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

