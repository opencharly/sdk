package agentkit

import "testing"

func TestRecoveryRequiresCompletedRCA(t *testing.T) {
	w := NewWorkflow()
	incident, err := w.RecordIncident("", "transport disconnected", []string{"transcript"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.DecideRecovery(incident.ID, "", "restart", false, nil); err == nil {
		t.Fatal("restart without RCA accepted")
	}
	rca, err := w.StartRCA(incident.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.DecideRecovery(incident.ID, rca.ID, "restart", false, nil); err == nil {
		t.Fatal("restart with active RCA accepted")
	}
	if _, err := w.CompleteRCA(rca.ID, "remote process exited", []string{"exit 1"}); err != nil {
		t.Fatal(err)
	}
	if _, err := w.DecideRecovery(incident.ID, rca.ID, "restart", false, nil); err != nil {
		t.Fatal(err)
	}
}

func TestEmergencyAbortIsExplicitException(t *testing.T) {
	w := NewWorkflow()
	incident, _ := w.RecordIncident("", "unsafe state", nil)
	if _, err := w.DecideRecovery(incident.ID, "", "abort", true, nil); err != nil {
		t.Fatal(err)
	}
}
