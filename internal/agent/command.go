package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// CommandRunner executes a whitelisted project command inside the project
// root. It never goes through a shell, so argument injection via `|`, `&&` or
// `$( )` is impossible by construction.
type CommandRunner interface {
	Run(ctx context.Context, root, command string, args []string) (string, error)
}

// allowedCommands are the only executables the agent may run. Everything else
// is rejected before exec.Command is constructed.
var allowedCommands = map[string]bool{
	"yarn": true, "npm": true, "pnpm": true,
	"node": true, "tsgo": true, "tsc": true, "eslint": true,
}

// forbiddenPackageSubcommands block package-manager subcommands with external
// or destructive side effects. They must go through the existing, confirmed
// project actions instead.
var forbiddenPackageSubcommands = map[string]bool{
	"publish": true, "unpublish": true, "login": true, "logout": true,
	"adduser": true, "init": true, "pack": true, "rebuild": true,
}

// lifecycleSubcommands are package-manager subcommands considered safe inside
// a project: dependency and script operations without publishing.
var lifecycleSubcommands = map[string]bool{
	"install": true, "add": true, "remove": true, "upgrade": true,
	"build": true, "test": true, "dev": true, "start": true,
	"run": true, "exec": true,
}

const (
	commandTimeout   = 5 * time.Minute
	maxCommandOutput = 100 * 1024
)

// commandRunner is the real implementation used by the web layer.
type commandRunner struct{}

// NewCommandRunner returns a CommandRunner backed by the local PATH.
func NewCommandRunner() CommandRunner { return commandRunner{} }

func (commandRunner) Run(ctx context.Context, root, command string, args []string) (string, error) {
	if !allowedCommands[command] {
		return "", fmt.Errorf("命令 %q 不在允许列表中", command)
	}
	if command == "yarn" || command == "npm" || command == "pnpm" {
		if err := validatePackageCommand(root, command, args); err != nil {
			return "", err
		}
	}
	if command == "node" {
		if err := validateNodeArgs(args); err != nil {
			return "", err
		}
	}
	timeoutCtx, cancel := context.WithTimeout(ctx, commandTimeout)
	defer cancel()
	cmd := exec.CommandContext(timeoutCtx, command, args...)
	cmd.Dir = root
	cmd.Stdin = nil
	var buffer bytes.Buffer
	cmd.Stdout = &buffer
	cmd.Stderr = &buffer
	runErr := cmd.Run()
	output := truncateOutput(buffer.String())
	if timeoutCtx.Err() == context.DeadlineExceeded {
		return output, fmt.Errorf("命令执行超时（超过 %s）", commandTimeout)
	}
	if runErr != nil {
		if code, ok := exitCode(runErr); ok {
			return output, fmt.Errorf("命令失败（退出码 %d）", code)
		}
		return output, fmt.Errorf("命令执行失败：%v", runErr)
	}
	return output, nil
}

// validatePackageCommand enforces that package-manager invocations are either
// standard lifecycle subcommands or `run <script>` where the script is
// declared in the project's package.json.
func validatePackageCommand(root, command string, args []string) error {
	if len(args) == 0 {
		return nil
	}
	if forbiddenPackageSubcommands[args[0]] {
		return fmt.Errorf("包管理器子命令 %q 被禁止；请通过受限的项目操作执行", args[0])
	}
	if lifecycleSubcommands[args[0]] {
		if args[0] == "run" || args[0] == "exec" {
			if len(args) < 2 {
				return errors.New("需要指定要运行的脚本名")
			}
			return validateScriptExists(root, args[1])
		}
		return nil
	}
	return fmt.Errorf("包管理器子命令 %q 不在允许范围", args[0])
}

// validateScriptExists rejects `run <script>` for scripts not declared in
// package.json, preventing `yarn run` from reaching arbitrary tool binaries.
func validateScriptExists(root, script string) error {
	raw, err := os.ReadFile(filepath.Join(root, "package.json"))
	if err != nil {
		return fmt.Errorf("无法读取 package.json：%v", err)
	}
	var manifest struct {
		Scripts map[string]string `json:"scripts"`
	}
	if err := json.Unmarshal(raw, &manifest); err != nil {
		return fmt.Errorf("package.json 无法解析：%v", err)
	}
	if _, ok := manifest.Scripts[script]; !ok {
		return fmt.Errorf("package.json 中没有脚本 %q；可用脚本：%s", script, strings.Join(sortedScripts(manifest.Scripts), ", "))
	}
	return nil
}

// validateNodeArgs limits node to syntax checks so it cannot execute arbitrary
// project scripts.
func validateNodeArgs(args []string) error {
	if len(args) == 0 {
		return errors.New("node 命令需要指定参数")
	}
	switch args[0] {
	case "--check", "--version", "-v":
		return nil
	default:
		return fmt.Errorf("node 仅允许 --check 语法检查，不允许执行脚本")
	}
}

func sortedScripts(scripts map[string]string) []string {
	keys := make([]string, 0, len(scripts))
	for name := range scripts {
		keys = append(keys, name)
	}
	sort.Strings(keys)
	return keys
}

func truncateOutput(output string) string {
	if len(output) <= maxCommandOutput {
		return output
	}
	return output[:maxCommandOutput] + "\n…（输出已截断）"
}

func exitCode(err error) (int, bool) {
	var exit *exec.ExitError
	if errors.As(err, &exit) {
		return exit.ExitCode(), true
	}
	return 0, false
}
