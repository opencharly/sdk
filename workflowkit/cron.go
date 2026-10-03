package workflowkit

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/robfig/cron/v3"
)

// cron.go — the ONE cron→systemd conversion. A pipeline's `triggers.schedule.cron`
// is a plain 5-field cron (the grammar deploy.schedule, a k8s CronJob, and GitHub
// Actions `on.schedule` all share); a timer-backed engine (plugin-lobster, via
// deploykit's GenerateSystemdTimer) needs a systemd `OnCalendar` expression. One
// implementation, so every consumer agrees.

// cronMacros expands the @-descriptors robfig's parser accepts into their 5-field
// form, so the converter only ever sees the numeric grammar.
var cronMacros = map[string]string{
	"@yearly":   "0 0 1 1 *",
	"@annually": "0 0 1 1 *",
	"@monthly":  "0 0 1 * *",
	"@weekly":   "0 0 * * 0",
	"@daily":    "0 0 * * *",
	"@midnight": "0 0 * * *",
	"@hourly":   "0 * * * *",
}

// mon..sun in cron's day-of-week order (0 = Sunday), for systemd's weekday names.
var cronDows = []string{"Sun", "Mon", "Tue", "Wed", "Thu", "Fri", "Sat"}

// CronToOnCalendar converts a 5-field cron expression (or an @-macro) into a systemd
// `OnCalendar=` value. It validates through robfig/cron's standard parser — the SAME
// grammar the rest of charly accepts — and renders the parsed bit-set, so lists, ranges,
// steps and names all lower correctly:
//
//	"0 3 * * *"             -> "*-*-* 03:00:00"
//	"*/15 9-17 * * Mon-Fri" -> "Mon,Tue,Wed,Thu,Fri *-*-* 09..17:00/15:00"
//	"0 0 1 * *"             -> "*-*-01 00:00:00"
//
// ONE cron form is REJECTED rather than lowered: a restricted day-of-month AND a
// restricted day-of-week in the same expression (`0 3 1 * 1`). cron fires when EITHER of
// those two fields matches; systemd's calendar spec requires BOTH. A single
// `OnCalendar=` line cannot express that OR, and GenerateSystemdTimer writes exactly one
// — so the conversion returns an error naming the cause instead of a schedule that fires
// on the wrong days. Either field restricted ALONE (`0 3 1 * *`, `0 3 * * 1`) is
// unaffected: an unrestricted field is not a restriction.
//
// The result is a valid OnCalendar expression; a caller may assert it with
// `systemd-analyze calendar`.
func CronToOnCalendar(expr string) (string, error) {
	src := strings.TrimSpace(expr)
	if src == "" {
		return "", fmt.Errorf("empty cron expression")
	}
	if m, ok := cronMacros[src]; ok {
		src = m
	}
	sched, err := cron.ParseStandard(src)
	if err != nil {
		return "", fmt.Errorf("cron %q: %w", expr, err)
	}
	ss, ok := sched.(*cron.SpecSchedule)
	if !ok {
		return "", fmt.Errorf("cron %q: unsupported schedule type %T", expr, sched)
	}

	minutes := bitValues(ss.Minute, 0, 59)
	hours := bitValues(ss.Hour, 0, 23)
	doms := bitValues(ss.Dom, 1, 31)
	months := bitValues(ss.Month, 1, 12)
	dows := bitValues(ss.Dow, 0, 6)

	if len(minutes) == 0 || len(hours) == 0 || len(doms) == 0 || len(months) == 0 || len(dows) == 0 {
		return "", fmt.Errorf("cron %q: empty field", expr)
	}

	domAll := len(doms) == 31
	monAll := len(months) == 12
	dowAll := len(dows) == 7

	// cron ORs day-of-month with day-of-week when BOTH are restricted; systemd ANDs them.
	// One OnCalendar= line cannot say "the 1st OR a Monday", so refuse rather than emit a
	// schedule that silently fires on the wrong days (R1: never paper over the divergence).
	if !domAll && !dowAll {
		fields := strings.Fields(src)
		domSrc, dowSrc := fields[2], fields[4]
		return "", fmt.Errorf(
			"cron %q: day-of-month %q and day-of-week %q are both restricted — cron fires when "+
				"EITHER matches, but systemd's OnCalendar= requires BOTH to match, so the two "+
				"cannot be expressed as a single OnCalendar= line; split it into two schedules "+
				"(one restricted by day-of-month, one by day-of-week)",
			expr, domSrc, dowSrc)
	}

	// time part: HH:MM:SS (zero-padded — systemd's canonical spelling).
	timePart := fmt.Sprintf("%s:%s:00", padTimeField(numField(hours, 0, 23)), padTimeField(numField(minutes, 0, 59)))

	// date part: [DOW] [YYYY-]MM-DD — omit wildcard components for readability.
	var datePart string
	switch {
	case domAll && monAll:
		datePart = "*-*-*"
	case domAll:
		datePart = "*-" + pad2List(months) + "-*"
	case monAll:
		datePart = "*-*-" + pad2List(doms)
	default:
		datePart = "*-" + pad2List(months) + "-" + pad2List(doms)
	}

	if dowAll {
		return datePart + " " + timePart, nil
	}
	names := make([]string, 0, len(dows))
	for _, d := range dows {
		names = append(names, cronDows[d])
	}
	return strings.Join(names, ",") + " " + datePart + " " + timePart, nil
}

// bitValues returns the sorted values set in a robfig cron bit-set within [lo, hi].
func bitValues(field uint64, lo, hi int) []int {
	var out []int
	for v := lo; v <= hi; v++ {
		if field&(uint64(1)<<uint(v)) != 0 {
			out = append(out, v)
		}
	}
	return out
}

// numField renders a numeric field the systemd way: `*` when the whole range is set,
// `lo/step` for a uniform step from the range start, else a comma list.
func numField(vals []int, lo, hi int) string {
	if len(vals) == hi-lo+1 {
		return "*"
	}
	if len(vals) > 2 {
		step := vals[1] - vals[0]
		if vals[0] == lo && step > 1 {
			uniform := true
			for i, v := range vals {
				if v != lo+i*step {
					uniform = false
					break
				}
			}
			if uniform && vals[len(vals)-1]+step > hi {
				return strconv.Itoa(lo) + "/" + strconv.Itoa(step)
			}
		}
		if step == 1 {
			return strconv.Itoa(vals[0]) + ".." + strconv.Itoa(vals[len(vals)-1])
		}
	}
	parts := make([]string, len(vals))
	for i, v := range vals {
		parts[i] = strconv.Itoa(v)
	}
	return strings.Join(parts, ",")
}

// padTimeField zero-pads every numeric component of a rendered minute/hour field
// (`9..17` -> `09..17`, `0/5` -> `00/5`), leaving `*` and the separators alone.
func padTimeField(s string) string {
	if s == "*" {
		return s
	}
	var b strings.Builder
	num := ""
	flush := func() {
		if num != "" {
			if len(num) == 1 {
				b.WriteByte('0')
			}
			b.WriteString(num)
			num = ""
		}
	}
	for _, r := range s {
		if r >= '0' && r <= '9' {
			num += string(r)
			continue
		}
		flush()
		b.WriteRune(r)
	}
	flush()
	return b.String()
}

// pad2List renders zero-padded, comma-joined values (systemd dates are 2-digit).
func pad2List(vals []int) string {
	parts := make([]string, len(vals))
	for i, v := range vals {
		parts[i] = fmt.Sprintf("%02d", v)
	}
	return strings.Join(parts, ",")
}
