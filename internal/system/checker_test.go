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

func TestWindowsRegistryPathValue(t *testing.T) {
	output := "\r\nHKEY_CURRENT_USER\\Environment\r\n    Path    REG_EXPAND_SZ    %LOCALAPPDATA%\\Programs\\nodejs;C:\\Tools\\Git\\cmd\r\n"
	got := windowsRegistryPathValue(output)
	want := "%LOCALAPPDATA%\\Programs\\nodejs;C:\\Tools\\Git\\cmd"
	if got != want {
		t.Fatalf("windowsRegistryPathValue() = %q, want %q", got, want)
	}
}

func TestExpandWindowsEnvironment(t *testing.T) {
	t.Setenv("LOCALAPPDATA", `C:\\Users\\alemon\\AppData\\Local`)
	got := expandWindowsEnvironment(`%LOCALAPPDATA%\\Programs\\nodejs`)
	want := `C:\\Users\\alemon\\AppData\\Local\\Programs\\nodejs`
	if got != want {
		t.Fatalf("expandWindowsEnvironment() = %q, want %q", got, want)
	}
}
