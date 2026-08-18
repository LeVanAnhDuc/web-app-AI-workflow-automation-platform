// Package schedule interprets the cron expressions of schedule triggers. Both
// the API (when activating a workflow) and the worker (when a schedule comes
// due) compute the next run, and they must agree, so the parser lives here
// rather than in either of them.
package schedule

import (
	"fmt"
	"time"

	"github.com/robfig/cron/v3"
)

// parser accepts the five-field format the schedule node documents:
// minute, hour, day of month, month, day of week.
var parser = cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow)

// Validate reports whether a cron expression is usable, without computing a
// time. Used to refuse activation early with a clear message.
func Validate(spec string) error {
	if _, err := parser.Parse(spec); err != nil {
		return fmt.Errorf("%q is not a valid cron expression: minute hour day-of-month month day-of-week", spec)
	}
	return nil
}

// NextRun returns the first run strictly after `after`, in UTC.
//
// An unknown timezone falls back to UTC rather than failing: the zone is user
// input typed into a node, and refusing to schedule at all would be a harsher
// answer than scheduling in the wrong zone and showing it.
func NextRun(spec, timezone string, after time.Time) (time.Time, error) {
	sched, err := parser.Parse(spec)
	if err != nil {
		return time.Time{}, fmt.Errorf("%q is not a valid cron expression", spec)
	}

	loc := time.UTC
	if timezone != "" {
		if parsed, err := time.LoadLocation(timezone); err == nil {
			loc = parsed
		}
	}
	return sched.Next(after.In(loc)).UTC(), nil
}
