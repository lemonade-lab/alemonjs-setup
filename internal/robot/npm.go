package robot

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

const npmRegistry = "https://registry.npmjs.org"

// NPMStatus is the information needed to safely guide a package publication.
// It never contains credentials or raw npm command output.
type NPMStatus struct {
	Name             string   `json:"name"`
	LocalVersion     string   `json:"localVersion"`
	LatestVersion    string   `json:"latestVersion,omitempty"`
	LatestPublished  string   `json:"latestPublished,omitempty"`
	Published        bool     `json:"published"`
	Private          bool     `json:"private"`
	LoggedIn         bool     `json:"loggedIn"`
	Username         string   `json:"username,omitempty"`
	SuggestedVersion string   `json:"suggestedVersion,omitempty"`
	Scripts          []string `json:"scripts"`
	Issues           []string `json:"issues"`
}

// NPMPackPreview is produced by npm itself, so the user sees the exact files
// that would be uploaded before credentials are used for publishing.
type NPMPackPreview struct {
	Name         string   `json:"name,omitempty"`
	Version      string   `json:"version,omitempty"`
	Filename     string   `json:"filename,omitempty"`
	FileCount    int      `json:"fileCount"`
	UnpackedSize int64    `json:"unpackedSize"`
	Files        []string `json:"files"`
}

type packageManifest struct {
	Name    string            `json:"name"`
	Version string            `json:"version"`
	Private bool              `json:"private"`
	Scripts map[string]string `json:"scripts"`
}

func (Manager) NPMStatus(root string) (NPMStatus, error) {
	path, err := projectPath(root)
	if err != nil {
		return NPMStatus{}, err
	}
	data, err := os.ReadFile(filepath.Join(path, "package.json"))
	if err != nil {
		return NPMStatus{}, fmt.Errorf("无法读取 package.json：%w", err)
	}
	var manifest packageManifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return NPMStatus{}, errors.New("package.json 格式无法识别，请先修正后再发布")
	}
	status := NPMStatus{Name: manifest.Name, LocalVersion: manifest.Version, Private: manifest.Private, Scripts: []string{}, Issues: []string{}}
	for _, name := range []string{"prepublishOnly", "prepack", "prepare", "build"} {
		if command := strings.TrimSpace(manifest.Scripts[name]); command != "" {
			status.Scripts = append(status.Scripts, name+": "+command)
		}
	}
	if status.Name == "" {
		status.Issues = append(status.Issues, "package.json 缺少包名，无法发布到 npm。")
	}
	if !semver(status.LocalVersion) {
		status.Issues = append(status.Issues, "本地版本号应为 1.2.3 这样的格式。")
	}
	if status.Private {
		status.Issues = append(status.Issues, "该项目标记为 private，npm 不允许发布。")
	}
	if status.Name != "" {
		latest, publishedAt, found, err := npmPackage(status.Name)
		if err != nil {
			status.Issues = append(status.Issues, "暂时无法连接 npm 官方仓库，发布前请重新检查。")
		} else if found {
			status.Published, status.LatestVersion, status.LatestPublished = true, latest, publishedAt
			if semver(status.LocalVersion) && compareSemver(status.LocalVersion, latest) <= 0 {
				status.Issues = append(status.Issues, "本地版本必须高于 npm 当前最新版 "+latest+"。")
			}
			status.SuggestedVersion = nextPatch(latest)
		} else {
			status.SuggestedVersion = status.LocalVersion
		}
	}
	if username := npmWhoami(path); username != "" {
		status.LoggedIn, status.Username = true, username
	} else {
		status.Issues = append(status.Issues, "尚未登录 npm；请先完成登录或配置发布令牌。")
	}
	return status, nil
}

func (Manager) NPMPackPreview(root string) (NPMPackPreview, error) {
	path, err := projectPath(root)
	if err != nil {
		return NPMPackPreview{}, err
	}
	// --ignore-scripts keeps this preview side-effect free. The actual npm
	// publish still runs the package lifecycle scripts and is shown separately.
	output, err := run(path, "npm", "pack", "--dry-run", "--json", "--ignore-scripts")
	if err != nil {
		return NPMPackPreview{}, fmt.Errorf("无法生成 npm 打包预览：%w", err)
	}
	var items []struct {
		Name         string `json:"name"`
		Version      string `json:"version"`
		Filename     string `json:"filename"`
		UnpackedSize int64  `json:"unpackedSize"`
		Files        []struct {
			Path string `json:"path"`
		} `json:"files"`
	}
	if err := json.Unmarshal([]byte(output), &items); err != nil || len(items) == 0 {
		return NPMPackPreview{}, errors.New("npm 未返回可识别的打包预览")
	}
	preview := NPMPackPreview{Name: items[0].Name, Version: items[0].Version, Filename: items[0].Filename, UnpackedSize: items[0].UnpackedSize, Files: []string{}}
	for _, item := range items[0].Files {
		preview.Files = append(preview.Files, item.Path)
	}
	preview.FileCount = len(preview.Files)
	return preview, nil
}

func npmPackage(name string) (latest, publishedAt string, found bool, err error) {
	client := &http.Client{Timeout: 8 * time.Second}
	response, err := client.Get(npmRegistry + "/" + url.PathEscape(name))
	if err != nil {
		return "", "", false, err
	}
	defer response.Body.Close()
	if response.StatusCode == http.StatusNotFound {
		return "", "", false, nil
	}
	if response.StatusCode != http.StatusOK {
		return "", "", false, fmt.Errorf("npm registry returned %s", response.Status)
	}
	var payload struct {
		DistTags map[string]string `json:"dist-tags"`
		Time     map[string]string `json:"time"`
	}
	if err := json.NewDecoder(response.Body).Decode(&payload); err != nil {
		return "", "", false, err
	}
	latest = payload.DistTags["latest"]
	if latest == "" {
		return "", "", false, nil
	}
	return latest, payload.Time[latest], true, nil
}

func npmWhoami(root string) string {
	cmd := exec.Command("npm", "whoami", "--registry="+npmRegistry)
	cmd.Dir = root
	output, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(output))
}

func publishWithToken(root, tag, token string) (string, error) {
	if strings.ContainsAny(token, "\r\n") {
		return "", errors.New("npm 令牌格式无效")
	}
	directory, err := os.MkdirTemp("", "alemonjs-setup-npm-")
	if err != nil {
		return "", fmt.Errorf("无法创建临时发布配置：%w", err)
	}
	defer os.RemoveAll(directory)
	config := filepath.Join(directory, ".npmrc")
	content := "registry=" + npmRegistry + "/\n//registry.npmjs.org/:_authToken=" + token + "\n"
	if err := os.WriteFile(config, []byte(content), 0600); err != nil {
		return "", fmt.Errorf("无法准备临时发布配置：%w", err)
	}
	return runWithEnv(root, map[string]string{"NPM_CONFIG_USERCONFIG": config}, "npm", "publish", "--tag", tag, "--registry="+npmRegistry)
}

var versionPattern = regexp.MustCompile(`^\d+\.\d+\.\d+$`)

func semver(version string) bool { return versionPattern.MatchString(version) }

func nextPatch(version string) string {
	parts := strings.Split(version, ".")
	if len(parts) != 3 {
		return ""
	}
	var major, minor, patch int
	if _, err := fmt.Sscanf(version, "%d.%d.%d", &major, &minor, &patch); err != nil {
		return ""
	}
	return fmt.Sprintf("%d.%d.%d", major, minor, patch+1)
}

func compareSemver(left, right string) int {
	var l1, l2, l3, r1, r2, r3 int
	if _, err := fmt.Sscanf(left, "%d.%d.%d", &l1, &l2, &l3); err != nil {
		return 0
	}
	if _, err := fmt.Sscanf(right, "%d.%d.%d", &r1, &r2, &r3); err != nil {
		return 0
	}
	for _, pair := range [][2]int{{l1, r1}, {l2, r2}, {l3, r3}} {
		if pair[0] < pair[1] {
			return -1
		}
		if pair[0] > pair[1] {
			return 1
		}
	}
	return 0
}
