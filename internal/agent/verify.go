package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

// verifyCandidates are package.json script names tried in order for the
// agent_verify tool. The first one present wins.
var verifyCandidates = []string{"check", "lint", "test", "build", "verify", "validate"}

// VerifyTool builds the agent_verify tool that runs the project's declared
// validation command. It falls back to the configured verify command when the
// project declares none of the known scripts.
func VerifyTool(root string, files FileService, commands CommandRunner, fallback CommandSpec) (Tool, Handler) {
	discovered := DiscoverVerifyCommands(root, files)
	if len(discovered) == 0 && fallback.Command != "" {
		discovered = []CommandSpec{fallback}
	}
	return Tool{
			Name:        verifyToolName,
			Description: "运行项目的验证命令（tsgo/tsc/eslint 或 package.json 中声明的 check/lint/test/build 脚本）并返回结果。写操作后会由 Agent 自动调用。",
			Parameters:  objectSchema(nil),
		}, func(ctx context.Context, arguments json.RawMessage) (string, error) {
			if len(discovered) == 0 {
				return "项目没有可用的验证命令。", nil
			}
			var outputs []string
			for index, spec := range discovered {
				output, err := commands.Run(ctx, root, spec.Command, spec.Args)
				label := fmt.Sprintf("验证步骤 %d（%s）", index+1, ValidateVerifySpec(spec))
				outputs = append(outputs, label+"\n"+output)
				if err != nil {
					return strings.Join(outputs, "\n\n") + "\n验证失败：" + err.Error(), err
				}
			}
			return strings.Join(outputs, "\n\n"), nil
		}
}

// CommandSpec is a whitelisted command with its fixed arguments.
type CommandSpec struct {
	Command string
	Args    []string
}

// DiscoverVerifyCommand inspects package.json scripts and picks the first
// verification candidate that exists. It never runs anything; it only resolves
// which declared script maps to a whitelisted subcommand.
func DiscoverVerifyCommand(root string, files FileService) (CommandSpec, bool) {
	commands := DiscoverVerifyCommands(root, files)
	if len(commands) == 0 {
		return CommandSpec{}, false
	}
	return commands[0], true
}

// DiscoverVerifyCommands resolves every safe verification script in priority
// order. Running a pipeline gives the model more useful feedback than
// stopping after the first successful check (for example, type-check then
// lint then tests). Duplicate command specifications are removed.
func DiscoverVerifyCommands(root string, files FileService) []CommandSpec {
	raw, err := files.ReadFile(root, "package.json")
	if err != nil {
		return nil
	}
	var manifest struct {
		Scripts map[string]string `json:"scripts"`
	}
	if err := json.Unmarshal([]byte(raw), &manifest); err != nil {
		return nil
	}
	var discovered []CommandSpec
	for _, candidate := range verifyCandidates {
		script, ok := manifest.Scripts[candidate]
		if !ok || script == "" {
			continue
		}
		// The script body is a package-manager run line we trust only for the
		// standard tools; anything exotic falls back to no verification rather
		// than executing arbitrary script text through a shell.
		command, args, ok := parseScriptInvocation(script)
		if ok {
			spec := CommandSpec{Command: command, Args: args}
			duplicate := false
			for _, existing := range discovered {
				if existing.Command == spec.Command && strings.Join(existing.Args, "\x00") == strings.Join(spec.Args, "\x00") {
					duplicate = true
					break
				}
			}
			if !duplicate {
				discovered = append(discovered, spec)
			}
		}
	}
	return discovered
}

// parseScriptInvocation interprets a package.json script body that begins with
// a whitelisted executable, returning that executable and its args. Shell
// operators are rejected. This only reinterprets `tsc --noEmit`-style scripts;
// `node build.js` style is rejected to avoid executing project code.
func parseScriptInvocation(script string) (string, []string, bool) {
	fields := splitFields(script)
	if len(fields) == 0 {
		return "", nil, false
	}
	switch fields[0] {
	case "tsc", "tsgo", "eslint", "node":
		if fields[0] == "node" {
			// Only syntax checks, never execute project scripts.
			if len(fields) >= 2 && fields[1] == "--check" {
				return "node", fields[1:], true
			}
			return "", nil, false
		}
		return fields[0], fields[1:], true
	}
	return "", nil, false
}

// splitFields splits on spaces, collapsing runs, and rejects any shell
// metacharacter so a script like `build:latest | sh` is refused.
func splitFields(script string) []string {
	var fields []string
	var current []rune
	for _, r := range script {
		switch r {
		case ' ', '\t':
			if len(current) > 0 {
				fields = append(fields, string(current))
				current = current[:0]
			}
		case '|', '&', ';', '>', '<', '$', '`', '(':
			return nil
		default:
			current = append(current, r)
		}
	}
	if len(current) > 0 {
		fields = append(fields, string(current))
	}
	return fields
}

// ValidateVerifySpec returns a ready-to-log description of the resolved verify
// command (used by the web layer for the system prompt).
func ValidateVerifySpec(spec CommandSpec) string {
	if spec.Command == "" {
		return "无"
	}
	return fmt.Sprintf("%s %s", spec.Command, joinArgs(spec.Args))
}

func joinArgs(args []string) string {
	out := ""
	for index, arg := range args {
		if index > 0 {
			out += " "
		}
		out += arg
	}
	return out
}
