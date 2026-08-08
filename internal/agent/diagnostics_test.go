package agent

import "testing"

func TestParseDiagnostic(t *testing.T) {
	d := ParseDiagnostic("/p", "src/index.ts:42:7 - error TS2322: Type mismatch")
	if d.ErrorKind != "typescript" || d.File != "src/index.ts" || d.Line != 42 || d.Column != 7 {
		t.Fatalf("诊断解析错误：%+v", d)
	}
}

func TestDiagnosticExplanation(t *testing.T) {
	text := diagnosticExplanation(DiagnosticContext{ErrorKind: "eslint"})
	if text == "" {
		t.Fatal("应返回 ESLint 修复建议")
	}
}
