package agent

// AgentRole is the audited role protocol used by future multi-agent runs.
type AgentRole string

const (
	RolePlanner         AgentRole = "planner"
	RoleExplorer        AgentRole = "explorer"
	RoleImplementer     AgentRole = "implementer"
	RoleTester          AgentRole = "tester"
	RoleReviewer        AgentRole = "reviewer"
	RoleSecurityAuditor AgentRole = "security_auditor"
)

type RoleMessage struct {
	TaskID   string    `json:"taskId"`
	Role     AgentRole `json:"role"`
	StepID   string    `json:"stepId,omitempty"`
	Kind     string    `json:"kind"`
	Payload  any       `json:"payload"`
	ReadOnly bool      `json:"readOnly"`
}

func RoleReadOnly(role AgentRole) bool {
	return role == RoleExplorer || role == RoleTester || role == RoleReviewer || role == RoleSecurityAuditor
}
