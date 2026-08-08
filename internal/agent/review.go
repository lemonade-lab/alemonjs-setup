package agent

import (
	"encoding/json"
	"fmt"
	"strings"

	"alemonx/internal/ai"
)

type ReviewResult struct {
	GoalSatisfied    bool     `json:"goalSatisfied"`
	Regressions      []string `json:"regressions,omitempty"`
	UnrelatedChanges []string `json:"unrelatedChanges,omitempty"`
	SecurityIssues   []string `json:"securityIssues,omitempty"`
	MissingTests     []string `json:"missingTests,omitempty"`
	Summary          string   `json:"summary"`
}

// ReviewTask performs deterministic read-only checks before a task is marked
// complete. It is intentionally conservative and provides the stable review
// contract that a model-backed reviewer can replace later.
func ReviewTask(plan TaskPlan, answer string) ReviewResult {
	result := ReviewResult{GoalSatisfied: true, Summary: "任务计划和验证流程已完成。"}
	if strings.TrimSpace(plan.Goal) == "" {
		result.GoalSatisfied = false
		result.MissingTests = append(result.MissingTests, "任务没有明确目标")
	}
	for _, step := range plan.Steps {
		if step.Status != "completed" {
			result.GoalSatisfied = false
			result.Regressions = append(result.Regressions, "步骤未完成："+step.Title)
		}
	}
	if strings.TrimSpace(answer) == "" {
		result.GoalSatisfied = false
		result.Regressions = append(result.Regressions, "Agent 没有返回最终结果")
	}
	if !result.GoalSatisfied {
		result.Summary = "任务未通过完成审查。"
	}
	return result
}

// ReviewTaskWithModel adds an independent read-only model pass after local
// checks. Invalid or unavailable model output safely falls back to the local
// result instead of blocking task completion unexpectedly.
func ReviewTaskWithModel(cfg ai.Resolved, plan TaskPlan, answer string, report string) ReviewResult {
	local := ReviewTask(plan, answer)
	if !local.GoalSatisfied {
		return local
	}
	prompt := fmt.Sprintf("你是只读代码审查员。仅返回 JSON：{\"goalSatisfied\":true/false,\"regressions\":[],\"unrelatedChanges\":[],\"securityIssues\":[],\"missingTests\":[],\"summary\":\"\"}。目标：%s\n计划：%s\n结果：%s\n审查材料：%s", plan.Goal, mustJSON(plan), answer, report)
	raw, err := ai.ChatResolved(cfg, []map[string]string{{"role": "system", "content": "你不能修改文件，只能审查。"}, {"role": "user", "content": prompt}})
	if err != nil {
		return local
	}
	start, end := strings.Index(raw, "{"), strings.LastIndex(raw, "}")
	if start < 0 || end <= start {
		return local
	}
	var result ReviewResult
	if json.Unmarshal([]byte(raw[start:end+1]), &result) != nil || result.Summary == "" {
		return local
	}
	return result
}

func mustJSON(value any) string { raw, _ := json.Marshal(value); return string(raw) }
