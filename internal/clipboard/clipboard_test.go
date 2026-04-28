package clipboard

import "testing"

func TestPickCommand_ReturnsSomethingOrNil(t *testing.T) {
	// Sanity check: shouldn't panic, returns either a non-empty slice or nil.
	got := pickCommand()
	if got != nil && len(got) == 0 {
		t.Fatalf("got empty slice")
	}
}
