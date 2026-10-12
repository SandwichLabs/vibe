package config

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/viper"
	"gopkg.in/yaml.v3"
)

// LLMSettings are the non-secret model settings Settings and `vibe config llm`
// can write to the config file. The API key is deliberately not here: it lives
// in the keychain, and SaveLLMSettings never writes or touches llm.api_key.
type LLMSettings struct {
	Provider  string `json:"provider"`
	Model     string `json:"model"`
	Endpoint  string `json:"endpoint"`
	AccountID string `json:"accountId"`
	Gateway   string `json:"gateway"`
}

// Validate checks every field. Empty optional fields are fine.
func (s LLMSettings) Validate() error {
	if _, ok := LLMProvider(s.Provider); !ok {
		return fmt.Errorf("unknown provider %q (want %s)", s.Provider, strings.Join(LLMProviderIDs(), ", "))
	}
	if err := ValidateModel(s.Model); err != nil {
		return err
	}
	if s.Endpoint != "" {
		if err := ValidateEndpoint(s.Endpoint); err != nil {
			return err
		}
	}
	if s.AccountID != "" {
		if err := ValidateAccountID(s.AccountID); err != nil {
			return err
		}
	}
	if s.Gateway != "" {
		if err := ValidateGatewayID(s.Gateway); err != nil {
			return err
		}
	}
	return nil
}

// LLMEnvOverrides maps each setting ("provider", "model", "endpoint",
// "accountId", "gateway") that an environment variable currently overrides to
// that variable's name, so a UI can say "set by VIBE_LLM_MODEL" and disable
// the field.
func LLMEnvOverrides() map[string]string {
	out := map[string]string{}
	for field, name := range map[string]string{
		"provider":  "VIBE_LLM_PROVIDER",
		"model":     "VIBE_LLM_MODEL",
		"endpoint":  "VIBE_LLM_ENDPOINT",
		"accountId": "VIBE_LLM_ACCOUNT_ID",
		"gateway":   "VIBE_LLM_GATEWAY",
	} {
		if strings.TrimSpace(os.Getenv(name)) != "" {
			out[field] = name
		}
	}
	return out
}

// ReadLLMSettings reads the settings as the config file at path holds them
// (no environment, no defaults). A missing file is empty settings.
func ReadLLMSettings(path string) LLMSettings {
	if path == "" {
		return LLMSettings{}
	}
	v := viper.New()
	v.SetConfigFile(path)
	v.SetConfigType("yaml")
	if v.ReadInConfig() != nil {
		return LLMSettings{}
	}
	return LLMSettings{
		Provider:  strings.TrimSpace(v.GetString("llm.provider")),
		Model:     strings.TrimSpace(v.GetString("llm.model")),
		Endpoint:  strings.TrimSpace(v.GetString("llm.endpoint")),
		AccountID: strings.TrimSpace(v.GetString("llm.account_id")),
		Gateway:   strings.TrimSpace(v.GetString("llm.gateway")),
	}
}

// SaveLLMSettingsKeepingEnv writes want to the file, except that a field an
// environment variable overrides keeps the file's own value (the form showed
// the environment's value, which must not be frozen into the file).
func SaveLLMSettingsKeepingEnv(path string, want LLMSettings) error {
	cur := ReadLLMSettings(path)
	ov := LLMEnvOverrides()
	if _, ok := ov["provider"]; ok {
		want.Provider = cur.Provider
	}
	if _, ok := ov["model"]; ok {
		want.Model = cur.Model
	}
	if _, ok := ov["endpoint"]; ok {
		want.Endpoint = cur.Endpoint
	}
	if _, ok := ov["accountId"]; ok {
		want.AccountID = cur.AccountID
	}
	if _, ok := ov["gateway"]; ok {
		want.Gateway = cur.Gateway
	}
	if want.Provider == "" {
		want.Provider = LLMProviderLocal
	}
	return SaveLLMSettings(path, want)
}

// SaveLLMSettings writes llm.provider, llm.model, llm.endpoint,
// llm.account_id and llm.gateway into the config file at path, keeping its
// comments and every other key. Empty model, endpoint, account and gateway
// remove their key (so defaults apply). The file is created (0600) if missing;
// a symlink is followed and the target edited; the write is atomic. It never
// reads, writes or removes llm.api_key.
func SaveLLMSettings(path string, s LLMSettings) error {
	s.Provider = strings.TrimSpace(s.Provider)
	s.Model = strings.TrimSpace(s.Model)
	s.Endpoint = strings.TrimSpace(s.Endpoint)
	s.AccountID = strings.TrimSpace(s.AccountID)
	s.Gateway = strings.TrimSpace(s.Gateway)
	if err := s.Validate(); err != nil {
		return err
	}
	if path == "" {
		return errors.New("there is no config file path")
	}
	mode := os.FileMode(0o600)
	var data []byte
	if _, lerr := os.Lstat(path); lerr == nil {
		if _, serr := os.Stat(path); serr != nil {
			return fmt.Errorf("failed to read %s: %w", path, serr) // a dangling symlink
		}
	}
	if real, err := filepath.EvalSymlinks(path); err == nil {
		path = real
		info, serr := os.Stat(path)
		if serr != nil {
			return fmt.Errorf("failed to stat %s: %w", path, serr)
		}
		mode = info.Mode().Perm()
		if data, err = os.ReadFile(path); err != nil {
			return fmt.Errorf("failed to read %s: %w", path, err)
		}
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("failed to resolve %s: %w", path, err)
	} else if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("failed to create %s: %w", filepath.Dir(path), err)
	}

	out, err := editLLMSettings(data, s)
	if err != nil {
		return fmt.Errorf("Vibe can't safely edit %s (%w); edit llm.* by hand", path, err)
	}
	if err := writeFileAtomic(path, out, mode); err != nil {
		return fmt.Errorf("failed to update %s: %w", path, err)
	}
	return nil
}

func editLLMSettings(data []byte, s LLMSettings) ([]byte, error) {
	root, out, err := parseSettingsRoot(data)
	if err != nil {
		return nil, err
	}
	for i := 0; i+1 < len(root.Content); i += 2 {
		if k := root.Content[i].Value; k != "llm" && strings.EqualFold(k, "llm") {
			return nil, fmt.Errorf("the key %q is not spelled llm", k)
		}
	}
	llm, err := llmMapping(root)
	if err != nil {
		return nil, err
	}
	setScalar(llm, "provider", s.Provider)
	setScalar(llm, "model", s.Model)
	setScalar(llm, "endpoint", s.Endpoint)
	setScalar(llm, "account_id", s.AccountID)
	setScalar(llm, "gateway", s.Gateway)
	return encodeSettings(out, data)
}

// parseSettingsRoot returns the top-level mapping of the settings file and the
// document node to encode (an empty file gets a new mapping).
func parseSettingsRoot(data []byte) (root, out *yaml.Node, err error) {
	if len(bytes.TrimSpace(data)) == 0 {
		root = &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
		return root, &yaml.Node{Kind: yaml.DocumentNode, Content: []*yaml.Node{root}}, nil
	}
	if n, err := yamlDocCount(data); err != nil || n != 1 {
		return nil, nil, errors.New("the file is not a single YAML document")
	}
	var doc yaml.Node
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return nil, nil, fmt.Errorf("the file is not valid YAML: %w", err)
	}
	if doc.Kind != yaml.DocumentNode || len(doc.Content) != 1 {
		return nil, nil, errors.New("unexpected YAML layout")
	}
	root = doc.Content[0]
	if root.Kind == yaml.ScalarNode && root.Tag == "!!null" {
		*root = yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
	}
	if root.Kind != yaml.MappingNode {
		return nil, nil, errors.New("the top level is not a mapping")
	}
	// Encode the whole document: header and trailing comments live on it.
	return root, &doc, nil
}

// llmMapping returns the llm: mapping of root (creating it), refusing shapes
// that cannot be edited without changing their meaning.
func llmMapping(root *yaml.Node) (*yaml.Node, error) {
	llm := mapChild(root, "llm")
	if llm != nil && (llm.Anchor != "" || llm.Kind == yaml.AliasNode) {
		return nil, errors.New("llm is an anchor or alias")
	}
	if llm == nil {
		llm = &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
		root.Content = append(root.Content, scalarNode("llm"), llm)
	} else if llm.Kind == yaml.ScalarNode && llm.Tag == "!!null" {
		*llm = yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
	}
	if llm.Kind != yaml.MappingNode {
		return nil, errors.New("llm is not a mapping")
	}
	for i := 0; i+1 < len(llm.Content); i += 2 {
		k, v := llm.Content[i].Value, llm.Content[i+1]
		if k != "api_key" && (v.Anchor != "" || v.Kind == yaml.AliasNode || (v.Kind != yaml.ScalarNode && isManaged(k))) {
			return nil, fmt.Errorf("llm.%s is an anchor, alias or not a plain value", k)
		}
		if k != strings.ToLower(k) && isManaged(strings.ToLower(k)) {
			return nil, fmt.Errorf("the key llm.%s is not spelled in lower case", k)
		}
	}
	return llm, nil
}

// encodeSettings encodes the edited document and checks that it still parses.
func encodeSettings(out *yaml.Node, orig []byte) ([]byte, error) {
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(out); err != nil {
		return nil, err
	}
	if err := enc.Close(); err != nil {
		return nil, err
	}
	outBytes := buf.Bytes()
	if bytes.HasPrefix(orig, []byte("---\n")) {
		outBytes = append([]byte("---\n"), outBytes...) // yaml.v3 drops an explicit document start
	}
	// The result must still parse and must not have changed the key.
	var chk map[string]any
	if err := yaml.Unmarshal(outBytes, &chk); err != nil {
		return nil, errors.New("the edit would leave an invalid file")
	}
	return outBytes, nil
}

func isManaged(k string) bool {
	switch k {
	case "provider", "model", "endpoint", "account_id", "gateway":
		return true
	}
	return false
}

func mapChild(m *yaml.Node, key string) *yaml.Node {
	for i := 0; i+1 < len(m.Content); i += 2 {
		if m.Content[i].Value == key {
			return m.Content[i+1]
		}
	}
	return nil
}

func scalarNode(v string) *yaml.Node {
	return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: v}
}

// setScalar sets key to value in a mapping, or removes the key when value is
// empty. An existing node keeps its comments.
func setScalar(m *yaml.Node, key, value string) {
	for i := 0; i+1 < len(m.Content); i += 2 {
		if m.Content[i].Value != key {
			continue
		}
		if value == "" {
			m.Content = append(m.Content[:i], m.Content[i+2:]...)
			return
		}
		n := m.Content[i+1]
		n.Kind, n.Tag, n.Value, n.Style, n.Alias, n.Content = yaml.ScalarNode, "!!str", value, 0, nil, nil
		return
	}
	if value != "" {
		m.Content = append(m.Content, scalarNode(key), scalarNode(value))
	}
}
