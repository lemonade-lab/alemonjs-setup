package web

import (
	"encoding/json"
	"io"
	"net/http"

	"alemonx/internal/agent"
	"alemonx/internal/robot"
)

func (s *server) agentDiagnosticsHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "该操作暂不支持。")
		return
	}
	var input struct {
		Root  string `json:"root"`
		Error string `json:"error"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&input); err != nil {
		writeError(w, http.StatusBadRequest, "请求无法识别。")
		return
	}
	if _, err := (robot.Manager{}).Validate(input.Root); err != nil {
		writeError(w, http.StatusBadRequest, "请先选择一个有效的机器人目录。")
		return
	}
	files := &robotFileService{manager: robot.Manager{}}
	if input.Error != "" {
		writeJSON(w, http.StatusOK, agent.ParseDiagnostic(input.Root, input.Error))
		return
	}
	spec, ok := agent.DiscoverVerifyCommand(input.Root, files)
	if !ok {
		writeJSON(w, http.StatusOK, agent.DiagnosticContext{Root: input.Root, ErrorKind: "unknown", ErrorText: "项目没有可用的验证命令。"})
		return
	}
	output, _ := agent.NewCommandRunner().Run(r.Context(), input.Root, spec.Command, spec.Args)
	writeJSON(w, http.StatusOK, agent.ParseDiagnostic(input.Root, output))
}
