package workflowkit

import (
	"strings"
	"testing"
)

func TestCronToOnCalendar(t *testing.T) {
	cases := []struct{ cron, want string }{
		{"0 3 * * *", "*-*-* 03:00:00"},
		{"30 2 * * *", "*-*-* 02:30:00"},
		{"0 * * * *", "*-*-* *:00:00"},
		{"0 0 1 * *", "*-*-01 00:00:00"},
		{"0 0 1 1 *", "*-01-01 00:00:00"},
		{"15 3 * * 1", "Mon *-*-* 03:15:00"},
		{"0 3 * * 1-5", "Mon,Tue,Wed,Thu,Fri *-*-* 03:00:00"},
		{"0 3 * * 0", "Sun *-*-* 03:00:00"},
		{"*/5 * * * *", "*-*-* *:00/05:00"},
		{"0 9-17 * * *", "*-*-* 09..17:00:00"},
		{"0 0,12 * * *", "*-*-* 00,12:00:00"},
		{"@daily", "*-*-* 00:00:00"},
		{"@hourly", "*-*-* *:00:00"},
		{"@weekly", "Sun *-*-* 00:00:00"},
		{"@monthly", "*-*-01 00:00:00"},
		{"@yearly", "*-01-01 00:00:00"},
		// The padded-step convention (padTimeField) on both fields, which is what the
		// doc example must show.
		{"*/15 9-17 * * Mon-Fri", "Mon,Tue,Wed,Thu,Fri *-*-* 09..17:00/15:00"},
		// Exactly ONE restricted day field lowers unchanged: an unrestricted field is
		// not a restriction (see TestCronToOnCalendarRejectsRestrictedDomAndDow).
		{"0 3 1 * *", "*-*-01 03:00:00"},
		{"0 3 * * 1", "Mon *-*-* 03:00:00"},
	}
	for _, tc := range cases {
		got, err := CronToOnCalendar(tc.cron)
		if err != nil {
			t.Errorf("CronToOnCalendar(%q): %v", tc.cron, err)
			continue
		}
		if got != tc.want {
			t.Errorf("CronToOnCalendar(%q) = %q, want %q", tc.cron, got, tc.want)
		}
	}
}

func TestCronToOnCalendarRejectsBadInput(t *testing.T) {
	for _, bad := range []string{"", "not a cron", "0 3 * *", "99 99 * * *", "0 3 * * * *"} {
		if got, err := CronToOnCalendar(bad); err == nil {
			t.Errorf("CronToOnCalendar(%q) = %q, want an error", bad, got)
		}
	}
}

// TestCronToOnCalendarRejectsRestrictedDomAndDow is the cron/systemd divergence guard.
// With BOTH day-of-month and day-of-week restricted, cron fires when EITHER matches while
// systemd requires BOTH, and one `OnCalendar=` line cannot express that OR
// (GenerateSystemdTimer writes exactly one). Lowering it silently produced a schedule
// that fires on the wrong days, so the conversion must refuse loudly instead.
func TestCronToOnCalendarRejectsRestrictedDomAndDow(t *testing.T) {
	got, err := CronToOnCalendar("0 3 1 * 1")
	if err == nil {
		t.Fatalf("CronToOnCalendar(%q) = %q with no error: cron ORs day-of-month/day-of-week "+
			"while systemd ANDs them, so a single OnCalendar= cannot express this", "0 3 1 * 1", got)
	}
	lower := strings.ToLower(err.Error())
	for _, want := range []string{"day-of-month", "day-of-week", "either", "both", "oncalendar", `"1"`} {
		if !strings.Contains(lower, want) {
			t.Errorf("error %q must name %q", err, want)
		}
	}
	if !strings.Contains(err.Error(), "0 3 1 * 1") {
		t.Errorf("error %q must quote the offending expression", err)
	}
	// The suggestion must be actionable: split into two schedules.
	if !strings.Contains(err.Error(), "two schedules") {
		t.Errorf("error %q must suggest splitting into two schedules", err)
	}
}
