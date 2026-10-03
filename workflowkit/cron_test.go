package workflowkit

import "testing"

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
