package agents

// paramJSONSchemaType maps a ParamDef.Type to its JSON Schema type name,
// defaulting unrecognized values to "string" (a permissive but valid schema).
func paramJSONSchemaType(t string) string {
	switch t {
	case "string", "boolean", "object", "array", "number", "integer":
		return t
	default:
		return "string"
	}
}

// paramSchemaNode renders one ParamDef as a JSON Schema node, recursively
// expanding array Items and object Fields.
func paramSchemaNode(p ParamDef) map[string]any {
	node := map[string]any{"type": paramJSONSchemaType(p.Type)}
	if p.Desc != "" {
		node["description"] = p.Desc
	}
	switch p.Type {
	case "array":
		// A concrete items schema is required here, not optional: strict JSON
		// Schema consumers (e.g. Gemini's function-calling validator) reject
		// {"type":"array"} with no "items". Default to a permissive string
		// item when the ToolDef didn't declare a shape.
		if p.Items != nil {
			node["items"] = paramSchemaNode(*p.Items)
		} else {
			node["items"] = map[string]any{"type": "string"}
		}
	case "object":
		if len(p.Fields) > 0 {
			props, required := ParamsToJSONSchema(p.Fields)
			node["properties"] = props
			if len(required) > 0 {
				node["required"] = required
			}
		} else {
			// Open-ended object params (run_query.params, create_record.values,
			// update_record.values, ...) have no fixed key set. An empty
			// "properties": {} previously emitted here is read by llama.cpp's
			// json-schema-to-grammar as "no keys allowed": it compiles a
			// grammar that can only produce {}, so a local model served
			// through llama.cpp can never send any params/values at all
			// (confirmed live against :8080 — run_query.params and
			// create_record.values/update_record.values always came back {}).
			// Emit a genuinely free-form object instead: no "properties" key,
			// additionalProperties: true. fantasy's Google/Gemini converter
			// drops additionalProperties, so this is also safe for Gemini.
			node["additionalProperties"] = true
		}
	}
	return node
}

// ParamsToJSONSchema converts a ToolDef's Params into JSON Schema
// "properties" + "required" lists. This is the single place a tool's input
// schema is built from its ParamDefs — internal/llm's fantasy tool wrapper
// (handlerTool.Info) and internal/mcp's MCP bridge (toolDefToMCPTool) both
// call it, so the schema the Studio wizard sees and the schema an external
// MCP client sees are byte-identical (#63 review fix).
func ParamsToJSONSchema(params []ParamDef) (map[string]any, []string) {
	properties := make(map[string]any, len(params))
	var required []string
	for _, p := range params {
		properties[p.Name] = paramSchemaNode(p)
		if p.Required {
			required = append(required, p.Name)
		}
	}
	return properties, required
}
