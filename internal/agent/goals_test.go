package agent

import "testing"

func TestGoalStoreRoundTrip(t *testing.T) {
	store := NewGoalStoreAt(t.TempDir())
	goal := Goal{ID: "g1", Prompt: "检查项目", Root: "/tmp", Status: GoalActive}
	if err := store.Save(goal); err != nil {
		t.Fatal(err)
	}
	loaded, err := store.Get("g1")
	if err != nil || loaded.Prompt != goal.Prompt {
		t.Fatalf("goal roundtrip failed: %+v %v", loaded, err)
	}
}

func TestGoalStoreReconcileMissingTask(t *testing.T) {
	store := NewGoalStoreAt(t.TempDir())
	if err := store.Save(Goal{ID: "g1", Prompt: "p", Root: "/tmp", Status: GoalActive}); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveRun(GoalRun{ID: "r1", GoalID: "g1", TaskID: "missing", Status: "running"}); err != nil {
		t.Fatal(err)
	}
	if err := store.ReconcileRuns(nil); err != nil {
		t.Fatal(err)
	}
	runs, _ := store.ListRuns("g1")
	if len(runs) != 1 || runs[0].Status != "failed" {
		t.Fatalf("unexpected runs: %+v", runs)
	}
}
