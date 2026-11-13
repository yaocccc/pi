package sessionview

import "testing"

func TestSnapshotCannotCommitToAnotherSession(t *testing.T) {
	var first, second State
	second.ObserveMessage(planMessage("second", "pending"), true)
	snapshot := first.NewSnapshot(nil)
	snapshot.ObserveMessage(planMessage("first", "completed"))
	revision := second.PlanRevision
	second.Commit(snapshot, true)
	status(t, &second, "pending")
	if second.PlanRevision != revision {
		t.Fatal("foreign snapshot changed revision")
	}
	first.Commit(snapshot, true)
	status(t, &first, "completed")
	second.Commit(nil, true)
	status(t, &second, "pending")
}
