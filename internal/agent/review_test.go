package agent

import "testing"

func TestReviewTaskRejectsIncompletePlan(t *testing.T) {
	plan := TaskPlan{Goal: "修复启动错误", Steps: []PlanStep{{ID: "fix", Status: "failed"}}}
	result := ReviewTask(plan, "已尝试修复")
	if result.GoalSatisfied || len(result.Regressions) == 0 {
		t.Fatalf("expected incomplete plan to fail review: %+v", result)
	}
}

func TestReviewTaskPassesCompletedPlan(t *testing.T) {
	plan := TaskPlan{Goal: "修复启动错误", Steps: []PlanStep{{ID: "fix", Status: "completed"}}}
	result := ReviewTask(plan, "问题已修复并通过验证")
	if !result.GoalSatisfied || result.Summary == "" {
		t.Fatalf("expected completed plan to pass review: %+v", result)
	}
}
