// Network & Firewall Setup plugin reference runner.
// It communicates with albs only through a JSON object on stdin/stdout.
package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
)

type request struct {
	Protocol string            `json:"protocol"`
	Method   string            `json:"method"`
	Action   string            `json:"action"`
	Params   map[string]string `json:"params"`
}
type response struct {
	Output string `json:"output,omitempty"`
	Error  string `json:"error,omitempty"`
}

func main() {
	var input request
	if err := json.NewDecoder(os.Stdin).Decode(&input); err != nil {
		write(response{Error: "无法读取 albs 插件请求：" + err.Error()})
		return
	}
	if input.Protocol != "albs.setup/v1" || input.Method != "run" {
		write(response{Error: "不支持的 albs Setup 插件协议"})
		return
	}
	output, err := execute(input.Action, input.Params)
	if err != nil {
		write(response{Output: output, Error: err.Error()})
		return
	}
	write(response{Output: output})
}

func write(value response) { _ = json.NewEncoder(os.Stdout).Encode(value) }

func execute(action string, params map[string]string) (string, error) {
	switch action {
	case "check":
		return networkCheck()
	case "firewall-status":
		return firewallStatus()
	case "open-port", "close-port":
		return changeFirewall(action, params)
	default:
		return "", errors.New("未知的网络与防火墙操作")
	}
}

func networkCheck() (string, error) {
	interfaces, err := networkInterfaces()
	if err != nil {
		return "", err
	}
	listeners, err := listeningPorts()
	if err != nil {
		return strings.Join(interfaces, "\n"), err
	}
	if listeners == "" {
		listeners = "未读取到正在监听的端口。"
	}
	return "网络地址\n" + strings.Join(interfaces, "\n") + "\n\n监听端口（最多 16 条）\n" + listeners, nil
}

func networkInterfaces() ([]string, error) {
	items, err := net.Interfaces()
	if err != nil {
		return nil, fmt.Errorf("无法读取网卡信息：%w", err)
	}
	result := make([]string, 0, len(items))
	for _, item := range items {
		if item.Flags&net.FlagUp == 0 || item.Flags&net.FlagLoopback != 0 {
			continue
		}
		addresses, _ := item.Addrs()
		values := make([]string, 0, len(addresses))
		for _, address := range addresses {
			values = append(values, address.String())
		}
		if len(values) > 0 {
			result = append(result, item.Name+" · "+strings.Join(values, ", "))
		}
	}
	if len(result) == 0 {
		return []string{"未发现已连接的非回环网络接口。"}, nil
	}
	return result, nil
}

func listeningPorts() (string, error) {
	switch runtime.GOOS {
	case "linux":
		return run("sh", "-lc", "(ss -H -ltnu 2>/dev/null || netstat -lntu 2>/dev/null) | head -n 16 || true")
	case "darwin":
		return run("sh", "-lc", "netstat -an -p tcp | grep LISTEN | head -n 16 || true")
	case "windows":
		return run("cmd", "/C", "netstat -ano | findstr LISTENING || exit /B 0")
	default:
		return "", fmt.Errorf("暂不支持 %s 的网络检查", runtime.GOOS)
	}
}

func firewallStatus() (string, error) {
	switch runtime.GOOS {
	case "linux":
		if available("ufw") {
			return run("ufw", "status", "verbose")
		}
		if available("firewall-cmd") {
			return run("sh", "-lc", "firewall-cmd --state && firewall-cmd --list-all")
		}
		if available("nft") {
			return run("nft", "list", "ruleset")
		}
		return "", errors.New("未发现 UFW、firewalld 或 nftables")
	case "darwin":
		return privileged("pfctl", "-s", "info")
	case "windows":
		return run("netsh", "advfirewall", "show", "allprofiles")
	default:
		return "", fmt.Errorf("暂不支持 %s 的防火墙检查", runtime.GOOS)
	}
}

func changeFirewall(action string, params map[string]string) (string, error) {
	port := params["port"]
	if port == "custom" {
		port = params["customPort"]
	}
	number, err := strconv.Atoi(port)
	if err != nil || number < 1 || number > 65535 {
		return "", errors.New("请选择 1 到 65535 之间的端口")
	}
	protocol := strings.ToLower(params["protocol"])
	if protocol != "tcp" && protocol != "udp" {
		return "", errors.New("请选择 TCP 或 UDP 协议")
	}
	if runtime.GOOS == "darwin" {
		return "", errors.New("macOS 当前仅支持查看 PF 防火墙状态；端口规则请通过系统网络设置管理")
	}
	var command string
	var args []string
	switch runtime.GOOS {
	case "linux":
		if available("ufw") {
			command = "ufw"
			if action == "open-port" {
				args = []string{"allow", fmt.Sprintf("%d/%s", number, protocol)}
			} else {
				args = []string{"delete", "allow", fmt.Sprintf("%d/%s", number, protocol)}
			}
		} else if available("firewall-cmd") {
			command = "firewall-cmd"
			verb := "add"
			if action == "close-port" {
				verb = "remove"
			}
			args = []string{fmt.Sprintf("--%s-port=%d/%s", verb, number, protocol), "--permanent"}
		} else {
			return "", errors.New("未发现受支持的 Linux 防火墙管理器（UFW 或 firewalld）")
		}
	case "windows":
		command = "netsh"
		name := fmt.Sprintf("ALBS %d/%s", number, protocol)
		if action == "open-port" {
			args = []string{"advfirewall", "firewall", "add", "rule", "name=" + name, "dir=in", "action=allow", "protocol=" + protocol, "localport=" + strconv.Itoa(number)}
		} else {
			args = []string{"advfirewall", "firewall", "delete", "rule", "name=" + name}
		}
	default:
		return "", fmt.Errorf("暂不支持 %s 的防火墙规则管理", runtime.GOOS)
	}
	output, err := privileged(command, args...)
	if err != nil {
		return output, err
	}
	verb := "已开放"
	if action == "close-port" {
		verb = "已关闭"
	}
	return fmt.Sprintf("%s %d/%s 入站端口规则。\n%s", verb, number, protocol, output), nil
}

func available(name string) bool { _, err := exec.LookPath(name); return err == nil }
func run(name string, args ...string) (string, error) {
	output, err := exec.Command(name, args...).CombinedOutput()
	text := strings.TrimSpace(string(output))
	if err != nil {
		if text == "" {
			text = err.Error()
		}
		return text, errors.New(text)
	}
	return text, nil
}
func privileged(name string, args ...string) (string, error) {
	switch runtime.GOOS {
	case "darwin":
		parts := append([]string{shellQuote(name)}, quoteAll(args)...)
		output, err := exec.Command("osascript", "-e", "do shell script "+strconv.Quote(strings.Join(parts, " "))+" with administrator privileges").CombinedOutput()
		return privilegeResult(output, err)
	case "linux":
		parts := append([]string{shellQuote(name)}, quoteAll(args)...)
		output, err := exec.Command("pkexec", "/bin/sh", "-lc", strings.Join(parts, " ")).CombinedOutput()
		return privilegeResult(output, err)
	case "windows":
		return "", errors.New("Windows 权限操作将在插件发布版中提供")
	default:
		return "", fmt.Errorf("暂不支持 %s 的系统权限操作", runtime.GOOS)
	}
}
func privilegeResult(output []byte, err error) (string, error) {
	text := strings.TrimSpace(string(output))
	if err != nil {
		if text == "" {
			text = "用户取消了系统权限授权，或系统拒绝了本次提升。"
		}
		return text, fmt.Errorf("需要管理员权限：%w", err)
	}
	return text, nil
}
func quoteAll(values []string) []string {
	result := make([]string, len(values))
	for i, value := range values {
		result[i] = shellQuote(value)
	}
	return result
}
func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\\\"'\\\"'") + "'"
}
