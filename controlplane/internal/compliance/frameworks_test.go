package compliance

import "testing"

func TestBSAAMLScreeningControlIsCatalogued(t *testing.T) {
	control, ok := ControlByID("BSA", "AML-SCREEN")
	if !ok {
		t.Fatal("expected BSA AML-SCREEN control")
	}
	if control.Title != "Customer sanctions screening" {
		t.Fatalf("unexpected title: %q", control.Title)
	}
}

func TestISO27001ScreeningRemainsPersonnelScoped(t *testing.T) {
	if _, ok := ControlByID("ISO27001", "A.6.1-AML"); ok {
		t.Fatal("ISO27001 A.6.1 should not be extended for customer AML screening")
	}
}
