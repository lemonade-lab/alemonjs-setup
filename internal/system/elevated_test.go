package system

import (
	"strings"
	"testing"
)

func TestPrivilegedScriptQuotesEachInput(t *testing.T) {
	script := privilegedScript("/tmp/a folder", map[string]string{"TOKEN": "a'; rm -rf /"}, "npm", []string{"publish", "--tag", "next"})
	for _, want := range []string{"cd -- '/tmp/a folder'", "'TOKEN=a\"'\"'; rm -rf /'", "'npm'", "'publish'"} {
		if !strings.Contains(script, want) {
			t.Fatalf("privileged script must quote %q: %s", want, script)
		}
	}
}

func TestWindowsScriptUsesLiteralPathsAndOutputFile(t *testing.T) {
	script := windowsScript(`C:\Robot's`, map[string]string{"NPM_CONFIG_USERCONFIG": `C:\Temp\a's.npmrc`}, "npm.cmd", []string{"publish", "--tag", "next"}, `C:\Temp\result.txt`)
	for _, want := range []string{"Set-Location -LiteralPath 'C:\\Robot''s'", "$env:NPM_CONFIG_USERCONFIG = 'C:\\Temp\\a''s.npmrc'", "WriteAllText('C:\\Temp\\result.txt'"} {
		if !strings.Contains(script, want) {
			t.Fatalf("windows script missing %q: %s", want, script)
		}
	}
}
