package agent

import (
	"strings"
	"testing"
)

func TestParseVerifyErrorsTsc(t *testing.T) {
	output := `src/index.ts:5:12 - error TS2322: Type 'string' is not assignable to type 'number'.

src/index.ts:9:3 - error TS2304: Cannot find name 'foo'.`
	errors := ParseVerifyErrors(output)
	if len(errors) != 2 {
		t.Fatalf("应解析 2 个错误，实际 %d：%+v", len(errors), errors)
	}
	if errors[0].Path != "src/index.ts" || errors[0].Line != 5 || errors[0].Column != 12 {
		t.Errorf("第一个错误字段错误：%+v", errors[0])
	}
	if !strings.Contains(errors[0].Message, "not assignable") {
		t.Errorf("错误消息错误：%q", errors[0].Message)
	}
}

func TestParseVerifyErrorsEslint(t *testing.T) {
	output := `/p/src/a.ts(10,2): error TS1005: ';' expected.`
	errors := ParseVerifyErrors(output)
	if len(errors) != 1 {
		t.Fatalf("应解析 1 个错误，实际 %d：%+v", len(errors), errors)
	}
	if errors[0].Line != 10 || errors[0].Column != 2 {
		t.Errorf("eslint 格式解析错误：%+v", errors[0])
	}
}

func TestParseVerifyErrorsNone(t *testing.T) {
	output := "检查通过，0 错误"
	if errors := ParseVerifyErrors(output); len(errors) != 0 {
		t.Errorf("无错误时不应解析出内容：%+v", errors)
	}
}

func TestFormatVerifyErrors(t *testing.T) {
	errors := []VerifyError{
		{Path: "src/a.ts", Line: 3, Column: 5, Message: "Type error"},
	}
	formatted := FormatVerifyErrors(errors)
	if formatted == "" || !strings.Contains(formatted, "src/a.ts:3:5") {
		t.Errorf("格式化错误：%q", formatted)
	}
	// 空列表 → 空字符串。
	if FormatVerifyErrors(nil) != "" {
		t.Error("空错误列表应返回空字符串")
	}
}
