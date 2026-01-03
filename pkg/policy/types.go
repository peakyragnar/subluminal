package policy

// PolicyDefaults defines top-level bundle defaults.
type PolicyDefaults struct {
	DecisionOnError   string `json:"decision_on_error,omitempty"`
	FailOpenReadTools *bool  `json:"fail_open_read_tools,omitempty"`
}

// WorkloadSelector matches workload context fields.
type WorkloadSelector struct {
	Namespace      []string          `json:"namespace,omitempty"`
	ServiceAccount []string          `json:"service_account,omitempty"`
	Labels         map[string]string `json:"labels,omitempty"`
	Repo           []string          `json:"repo,omitempty"`
	Branch         []string          `json:"branch,omitempty"`
}

// PolicySelectors gates policy application by identity/workload context.
type PolicySelectors struct {
	Env      []string          `json:"env,omitempty"`
	AgentID  []string          `json:"agent_id,omitempty"`
	Client   []string          `json:"client,omitempty"`
	Workload *WorkloadSelector `json:"workload,omitempty"`
}

// IsZero reports whether no selector fields are specified.
func (s PolicySelectors) IsZero() bool {
	if len(s.Env) > 0 || len(s.AgentID) > 0 || len(s.Client) > 0 {
		return false
	}
	if s.Workload == nil {
		return true
	}
	if len(s.Workload.Namespace) > 0 || len(s.Workload.ServiceAccount) > 0 || len(s.Workload.Repo) > 0 || len(s.Workload.Branch) > 0 {
		return false
	}
	return len(s.Workload.Labels) == 0
}

// SelectorTarget describes the identity/workload context for selector matching.
type SelectorTarget struct {
	Env      string
	AgentID  string
	Client   string
	Workload WorkloadContext
}

// WorkloadContext describes optional workload metadata.
type WorkloadContext struct {
	Namespace      string            `json:"namespace"`
	ServiceAccount string            `json:"service_account"`
	Labels         map[string]string `json:"labels"`
	Repo           string            `json:"repo"`
	Branch         string            `json:"branch"`
}

// ArgsMatch defines argument predicates for rule matching.
type ArgsMatch struct {
	HasKeys      []string                `json:"has_keys,omitempty"`
	KeyEquals    map[string]any          `json:"key_equals,omitempty"`
	KeyIn        map[string][]any        `json:"key_in,omitempty"`
	NumericRange map[string]NumericRange `json:"numeric_range,omitempty"`
}

// IsZero reports whether no argument predicates are specified.
func (m *ArgsMatch) IsZero() bool {
	if m == nil {
		return true
	}
	if len(m.HasKeys) > 0 || len(m.KeyEquals) > 0 || len(m.KeyIn) > 0 || len(m.NumericRange) > 0 {
		return false
	}
	return true
}

// NumericRange specifies an inclusive numeric range.
type NumericRange struct {
	Min *float64 `json:"min,omitempty"`
	Max *float64 `json:"max,omitempty"`
}

// BundleSpec is the authoring/parsed policy bundle.
type BundleSpec struct {
	PolicyID      string          `json:"policy_id"`
	PolicyVersion string          `json:"policy_version,omitempty"`
	Version       string          `json:"version,omitempty"`
	PolicyHash    string          `json:"policy_hash,omitempty"`
	Mode          string          `json:"mode"`
	Defaults      PolicyDefaults  `json:"defaults,omitempty"`
	Selectors     PolicySelectors `json:"selectors,omitempty"`
	Rules         []Rule          `json:"rules"`
	Description   string          `json:"description,omitempty"`
	Owner         string          `json:"owner,omitempty"`
	CreatedAt     string          `json:"created_at,omitempty"`
}

// EffectiveVersion resolves the version field precedence.
func (s BundleSpec) EffectiveVersion() string {
	if s.PolicyVersion != "" {
		return s.PolicyVersion
	}
	return s.Version
}

// CompiledBundle is the compiled snapshot and runtime bundle.
type CompiledBundle struct {
	Bundle   Bundle
	Snapshot []byte
	Hash     string
}
