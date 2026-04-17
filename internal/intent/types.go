package intent

// Requirement matches specs/requirements/*.yaml.
type Requirement struct {
	ID       string   `yaml:"id"`
	Title    string   `yaml:"title"`
	Why      string   `yaml:"why"`
	ScopeIn  []string `yaml:"scope_in"`
	ScopeOut []string `yaml:"scope_out"`
	Gates    []Gate   `yaml:"gates"`

	// Populated by Load: the path the requirement was loaded from + raw bytes.
	SpecPath string `yaml:"-"`
	RawYAML  []byte `yaml:"-"`
}

// Gate is a requirement-scoped acceptance gate.
type Gate struct {
	ID       string   `yaml:"id"`
	Kind     string   `yaml:"kind"`     // test|property|rubric|human|custom
	Producer Producer `yaml:"producer"`
}

// Producer identifies who/what produces the verdict and how.
type Producer struct {
	Kind   string         `yaml:"kind"`   // executable|human|agent|pipeline
	Config map[string]any `yaml:"config"`
}

// Task matches specs/tasks/*.yaml.
type Task struct {
	ID            string   `yaml:"id"`
	Implements    []string `yaml:"implements"`
	DependsOn     []string `yaml:"depends_on"`
	RequiredGates []string `yaml:"required_gates"`

	SpecPath string `yaml:"-"`
	RawYAML  []byte `yaml:"-"`
}

// Bundle is the loaded spec tree.
type Bundle struct {
	Requirements []Requirement
	Tasks        []Task
}
