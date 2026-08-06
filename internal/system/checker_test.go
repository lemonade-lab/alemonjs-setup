package system

import "testing"

func TestGitBuildCheckDoesNotRequireGlobalPackageTools(t *testing.T) {
	report := NewChecker().CheckGoal("build", "git")
	ids := map[string]bool{}
	for _, check := range report.Checks {
		ids[check.ID] = true
	}
	if !ids["node"] || !ids["git"] {
		t.Fatalf("git build checks = %#v, want node and git", report.Checks)
	}
	for _, id := range []string{"yarn", "pnpm", "jq"} {
		if ids[id] {
			t.Fatalf("git build should not require global %s: %#v", id, report.Checks)
		}
	}
}
