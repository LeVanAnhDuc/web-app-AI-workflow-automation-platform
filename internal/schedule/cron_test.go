package schedule

import (
	"strings"
	"testing"
	"time"
)

func TestValidateAcceptsFiveFieldExpressions(t *testing.T) {
	for _, spec := range []string{"0 9 * * *", "*/5 * * * *", "30 3 1 * *", "0 0 * * 1-5"} {
		if err := Validate(spec); err != nil {
			t.Fatalf("Validate(%q): %v", spec, err)
		}
	}
}

func TestValidateRejectsNonsenseWithAHelpfulMessage(t *testing.T) {
	err := Validate("every tuesday")
	if err == nil {
		t.Fatal("expected an error")
	}
	// The message ends up in the UI next to the field, so it has to say what
	// the five fields are.
	if !strings.Contains(err.Error(), "day-of-week") {
		t.Fatalf("message %q does not describe the expected format", err)
	}
}

func TestValidateRejectsSixFieldExpressions(t *testing.T) {
	// A seconds field is a common paste from another tool; accepting it would
	// silently shift every run by a factor of sixty.
	if err := Validate("0 0 9 * * *"); err == nil {
		t.Fatal("expected the six-field form to be rejected")
	}
}

func TestNextRunIsStrictlyAfterTheGivenTime(t *testing.T) {
	at9 := time.Date(2026, 8, 18, 9, 0, 0, 0, time.UTC)

	next, err := NextRun("0 9 * * *", "UTC", at9)
	if err != nil {
		t.Fatalf("NextRun: %v", err)
	}
	want := at9.AddDate(0, 0, 1)
	if !next.Equal(want) {
		t.Fatalf("next run %v, want %v — a run must not fire twice for the same minute", next, want)
	}
}

func TestNextRunReturnsUTC(t *testing.T) {
	// 09:00 in Ho Chi Minh City is 02:00 UTC; the stored value must be UTC so
	// the worker's comparison against now() is unambiguous.
	after := time.Date(2026, 8, 18, 0, 0, 0, 0, time.UTC)

	next, err := NextRun("0 9 * * *", "Asia/Ho_Chi_Minh", after)
	if err != nil {
		t.Fatalf("NextRun: %v", err)
	}
	if next.Location() != time.UTC {
		t.Fatalf("location %v, want UTC", next.Location())
	}
	if next.Hour() != 2 {
		t.Fatalf("hour %d, want 02 UTC for 09:00 +07", next.Hour())
	}
}

func TestNextRunFallsBackToUTCForAnUnknownZone(t *testing.T) {
	after := time.Date(2026, 8, 18, 0, 0, 0, 0, time.UTC)

	next, err := NextRun("0 9 * * *", "Mars/Olympus_Mons", after)
	if err != nil {
		t.Fatalf("an unknown zone is user input, not a fault: %v", err)
	}
	if next.Hour() != 9 {
		t.Fatalf("hour %d, want the UTC interpretation", next.Hour())
	}
}

func TestNextRunRejectsABadExpression(t *testing.T) {
	if _, err := NextRun("nope", "UTC", time.Now()); err == nil {
		t.Fatal("expected an error for an invalid expression")
	}
}

func TestNextRunHandlesAStepSchedule(t *testing.T) {
	after := time.Date(2026, 8, 18, 10, 2, 30, 0, time.UTC)

	next, err := NextRun("*/5 * * * *", "UTC", after)
	if err != nil {
		t.Fatalf("NextRun: %v", err)
	}
	want := time.Date(2026, 8, 18, 10, 5, 0, 0, time.UTC)
	if !next.Equal(want) {
		t.Fatalf("next run %v, want %v", next, want)
	}
}
