package host

import (
	"errors"
	"testing"
)

func TestValidateRuntimeReportRequiresClosedSupportedSet(t *testing.T) {
	t.Parallel()

	valid := []RuntimeObservation{{Kind: RuntimePi}, {Kind: RuntimeCodex}}
	if err := ValidateRuntimeReport(valid); err != nil {
		t.Fatalf("valid report rejected: %v", err)
	}
	for _, invalid := range [][]RuntimeObservation{
		{{Kind: RuntimePi}},
		{{Kind: RuntimePi}, {Kind: RuntimePi}},
		{{Kind: RuntimePi}, {Kind: RuntimeKind("other")}},
	} {
		if err := ValidateRuntimeReport(invalid); !errors.Is(err, ErrInvalidRuntimeReport) {
			t.Errorf("invalid report error = %v", err)
		}
	}
}
