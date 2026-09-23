package agents

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestParamSchemaNode_OpenEndedObject pins the exact JSON Schema shape
// emitted for an object ParamDef with no fixed Fields (run_query.params,
// create_record.values, update_record.values, ...). It must NOT include a
// "properties" key: llama.cpp's json-schema-to-grammar reads an empty
// "properties": {} as "no keys allowed" and compiles a grammar that can only
// produce {}, which made these params/values unusable against a local model
// served through llama.cpp (confirmed live against :8080).
func TestParamSchemaNode_OpenEndedObject(t *testing.T) {
	p := ParamDef{Name: "params", Type: "object", Desc: "Named parameters for the query"}
	node := paramSchemaNode(p)

	assert.Equal(t, "object", node["type"])
	assert.Equal(t, "Named parameters for the query", node["description"])
	assert.Equal(t, true, node["additionalProperties"])
	_, hasProperties := node["properties"]
	assert.False(t, hasProperties, "open-ended object schema must not declare a properties key")
}

// TestParamsToJSONSchema_RunQueryParams pins the shape of the actual
// run_query tool's "params" field as it appears in the registered ToolDef.
func TestParamsToJSONSchema_RunQueryParams(t *testing.T) {
	def, ok := findToolDef(t, "run_query")
	require.True(t, ok, "run_query tool must be registered")

	props, _ := ParamsToJSONSchema(def.Params)
	paramsSchema, ok := props["params"].(map[string]any)
	require.True(t, ok, "run_query must declare a params field")

	assert.Equal(t, map[string]any{
		"type":                 "object",
		"description":          "Named parameters for the query",
		"additionalProperties": true,
	}, paramsSchema)
}

// TestParamsToJSONSchema_CreateRecordValues pins the shape of the actual
// create_record tool's "values" field as it appears in the registered
// ToolDef.
func TestParamsToJSONSchema_CreateRecordValues(t *testing.T) {
	def, ok := findToolDef(t, "create_record")
	require.True(t, ok, "create_record tool must be registered")

	props, _ := ParamsToJSONSchema(def.Params)
	valuesSchema, ok := props["values"].(map[string]any)
	require.True(t, ok, "create_record must declare a values field")

	assert.Equal(t, map[string]any{
		"type":                 "object",
		"description":          "Column-value pairs for the new record",
		"additionalProperties": true,
	}, valuesSchema)
}

// findToolDef locates a registered ToolDef by name in the package's Tools
// registry. It fails the test loudly if the registry shape changes out from
// under it rather than silently skipping.
func findToolDef(t *testing.T, name string) (ToolDef, bool) {
	t.Helper()
	for _, def := range Tools {
		if def.Name == name {
			return def, true
		}
	}
	return ToolDef{}, false
}
