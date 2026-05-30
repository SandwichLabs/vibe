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

export namespace main {
	
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
	export class ColumnInfo {
	    name: string;
	    type: string;
	    not_null: boolean;
	    pk: boolean;
	
	    static createFrom(source: any = {}) {
	        return new ColumnInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.type = source["type"];
	        this.not_null = source["not_null"];
	        this.pk = source["pk"];
	    }
	}
	export class DashboardData {
	    title: string;
	    description: string;
	    page_count: number;
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
	        this.page_count = source["page_count"];
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
	    page_count: number;
	    table_count: number;
	    query_count: number;
	    file_size: number;
	
	    static createFrom(source: any = {}) {
	        return new FileInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.path = source["path"];
	        this.title = source["title"];
	        this.description = source["description"];
	        this.page_count = source["page_count"];
	        this.table_count = source["table_count"];
	        this.query_count = source["query_count"];
	        this.file_size = source["file_size"];
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
	export class ForeignKeySpec {
	    child_table: string;
	    child_column: string;
	    parent_table: string;
	    parent_column: string;
	    on_delete: string;
	    on_update: string;
	
	    static createFrom(source: any = {}) {
	        return new ForeignKeySpec(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.child_table = source["child_table"];
	        this.child_column = source["child_column"];
	        this.parent_table = source["parent_table"];
	        this.parent_column = source["parent_column"];
	        this.on_delete = source["on_delete"];
	        this.on_update = source["on_update"];
	    }
	}
	export class FormSubmitRow {
	    table: string;
	    values: Record<string, any>;
	
	    static createFrom(source: any = {}) {
	        return new FormSubmitRow(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.table = source["table"];
	        this.values = source["values"];
	    }
	}
	export class FormSummary {
	    id: string;
	    title: string;
	    archetype: string;
	
	    static createFrom(source: any = {}) {
	        return new FormSummary(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.title = source["title"];
	        this.archetype = source["archetype"];
	    }
	}
	export class ImportResult {
	    rows_imported: number;
	    table_name: string;
	
	    static createFrom(source: any = {}) {
	        return new ImportResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.rows_imported = source["rows_imported"];
	        this.table_name = source["table_name"];
	    }
	}
	export class PageDetail {
	    id: string;
	    title: string;
	    content: string;
	    page_type: string;
	    sort_order: number;
	
	    static createFrom(source: any = {}) {
	        return new PageDetail(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.title = source["title"];
	        this.content = source["content"];
	        this.page_type = source["page_type"];
	        this.sort_order = source["sort_order"];
	    }
	}
	export class PageSummary {
	    id: string;
	    title: string;
	    page_type: string;
	    sort_order: number;
	
	    static createFrom(source: any = {}) {
	        return new PageSummary(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.title = source["title"];
	        this.page_type = source["page_type"];
	        this.sort_order = source["sort_order"];
	    }
	}
	export class QueryResult {
	    columns: ColumnInfo[];
	    rows: any[][];
	    duration: string;
	    row_count: number;
	
	    static createFrom(source: any = {}) {
	        return new QueryResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.columns = this.convertValues(source["columns"], ColumnInfo);
	        this.rows = source["rows"];
	        this.duration = source["duration"];
	        this.row_count = source["row_count"];
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
	export class TableData {
	    columns: string[];
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

export namespace schema {
	
	export class FieldMeta {
	    table_name: string;
	    column_name: string;
	    caption: string;
	    control_type: string;
	    default_value: string;
	    required: boolean;
	    format_code: string;
	    validation_rule: string;
	    validation_message: string;
	    input_mask: string;
	    lookup_sql: string;
	    display_col: string;
	    value_col: string;
	    control_tip: string;
	    sort_order: number;
	
	    static createFrom(source: any = {}) {
	        return new FieldMeta(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.table_name = source["table_name"];
	        this.column_name = source["column_name"];
	        this.caption = source["caption"];
	        this.control_type = source["control_type"];
	        this.default_value = source["default_value"];
	        this.required = source["required"];
	        this.format_code = source["format_code"];
	        this.validation_rule = source["validation_rule"];
	        this.validation_message = source["validation_message"];
	        this.input_mask = source["input_mask"];
	        this.lookup_sql = source["lookup_sql"];
	        this.display_col = source["display_col"];
	        this.value_col = source["value_col"];
	        this.control_tip = source["control_tip"];
	        this.sort_order = source["sort_order"];
	    }
	}
	export class FormFieldSpec {
	    name: string;
	    caption: string;
	    control_type: string;
	
	    static createFrom(source: any = {}) {
	        return new FormFieldSpec(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.caption = source["caption"];
	        this.control_type = source["control_type"];
	    }
	}
	export class SubformSpec {
	    child_table: string;
	    child_key: string;
	    parent_key: string;
	    fields: FormFieldSpec[];
	
	    static createFrom(source: any = {}) {
	        return new SubformSpec(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.child_table = source["child_table"];
	        this.child_key = source["child_key"];
	        this.parent_key = source["parent_key"];
	        this.fields = this.convertValues(source["fields"], FormFieldSpec);
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
	export class FormSpec {
	    id: string;
	    title: string;
	    table: string;
	    archetype: string;
	    on_submit: string;
	    fields: FormFieldSpec[];
	    subform?: SubformSpec;
	
	    static createFrom(source: any = {}) {
	        return new FormSpec(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.title = source["title"];
	        this.table = source["table"];
	        this.archetype = source["archetype"];
	        this.on_submit = source["on_submit"];
	        this.fields = this.convertValues(source["fields"], FormFieldSpec);
	        this.subform = this.convertValues(source["subform"], SubformSpec);
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
	export class Relationship {
	    id: string;
	    parent_table: string;
	    parent_column: string;
	    child_table: string;
	    child_column: string;
	    on_delete: string;
	    on_update: string;
	    enforce_integrity: boolean;
	    created_at: string;
	
	    static createFrom(source: any = {}) {
	        return new Relationship(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.parent_table = source["parent_table"];
	        this.parent_column = source["parent_column"];
	        this.child_table = source["child_table"];
	        this.child_column = source["child_column"];
	        this.on_delete = source["on_delete"];
	        this.on_update = source["on_update"];
	        this.enforce_integrity = source["enforce_integrity"];
	        this.created_at = source["created_at"];
	    }
	}

}

