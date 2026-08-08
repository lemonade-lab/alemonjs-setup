package agent

import (
	"errors"
	"regexp"
	"strings"
)

// RecoverableError represents an infrastructure failure for which the task
// state can safely be retained and resumed. Error deliberately contains only
// a user-safe explanation; Cause is kept for server-side logging.
type RecoverableError struct {
	Public string
	Cause  error
}

func (e *RecoverableError) Error() string {
	if e == nil || e.Public == "" {
		return "任务已暂停，可稍后继续执行。"
	}
	return e.Public
}

func (e *RecoverableError) Unwrap() error { return e.Cause }

// IsRecoverable reports whether a runner failure should pause rather than
// discard a task. The original cause remains available through errors.As for
// diagnostics, but must never be sent to the browser.
func IsRecoverable(err error) bool {
	var recoverable *RecoverableError
	return errors.As(err, &recoverable)
}

// VerifyError is one compiler/linter diagnostic extracted from a verify
// command's output.
type VerifyError struct {
	Path    string `json:"path"`
	Line    int    `json:"line"`
	Column  int    `json:"column"`
	Message string `json:"message"`
}

// errorPattern matches tsc / eslint style diagnostics:
//
//	/path/to/file.ts:12:34 - error TS2322: Type 'x' is not assignable.
//	src/a.ts(5,10): error TS1005: ';' expected.
var errorPattern = regexp.MustCompile(`([^\s:]+):(\d+):(\d+)\s*-\s*(error|warning)\s+[^:]*:\s*(.+)`)

var parenPattern = regexp.MustCompile(`([^\s:(]+)\((\d+),(\d+)\):\s*(error|warning)\s+[^:]*:\s*(.+)`)

// ParseVerifyErrors extracts structured diagnostics from a verify command's
// output. It returns an empty slice when the output has no matchable errors.
func ParseVerifyErrors(output string) []VerifyError {
	var errors []VerifyError
	for _, line := range strings.Split(output, "\n") {
		if error := parseErrorLine(line); error != nil {
			errors = append(errors, *error)
		}
	}
	return errors
}

func parseErrorLine(line string) *VerifyError {
	if match := parenPattern.FindStringSubmatch(line); match != nil {
		return &VerifyError{
			Path:    match[1],
			Line:    atoi(match[2]),
			Column:  atoi(match[3]),
			Message: strings.TrimSpace(match[5]),
		}
	}
	if match := errorPattern.FindStringSubmatch(line); match != nil {
		return &VerifyError{
			Path:    match[1],
			Line:    atoi(match[2]),
			Column:  atoi(match[3]),
			Message: strings.TrimSpace(match[5]),
		}
	}
	return nil
}

func atoi(value string) int {
	result := 0
	for _, r := range value {
		if r < '0' || r > '9' {
			break
		}
		result = result*10 + int(r-'0')
	}
	return result
}

// FormatVerifyErrors renders structured diagnostics into a compact summary the
// model can act on. It returns "" when there are no errors.
func FormatVerifyErrors(errors []VerifyError) string {
	if len(errors) == 0 {
		return ""
	}
	var builder strings.Builder
	builder.WriteString("验证发现以下问题，请修复后重新验证：\n")
	for _, item := range errors {
		builder.WriteString("- ")
		builder.WriteString(item.Path)
		builder.WriteString(":")
		builder.WriteString(itoa(item.Line))
		builder.WriteString(":")
		builder.WriteString(itoa(item.Column))
		builder.WriteString(" ")
		builder.WriteString(item.Message)
		builder.WriteString("\n")
	}
	return builder.String()
}

func itoa(value int) string {
	if value == 0 {
		return "0"
	}
	var digits []byte
	for value > 0 {
		digits = append([]byte{byte('0' + value%10)}, digits...)
		value /= 10
	}
	return string(digits)
}
