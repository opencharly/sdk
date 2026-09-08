package kit

import "github.com/opencharly/spec/spec"

// description_merge.go — P12a follow-up: MergeDeployDescriptions relocated from
// charly/description_collect.go. Pure over LabelDescriptionSet/LabeledDescription/spec.Step
// (already kit-native types, see planrun.go) — zero core state. Its sole caller, the "live"
// gather engine's pod path, is plugin-side too (candy/plugin-check/live_gather.go's
// pluginCheckLivePod, K1-unblock wave arm 1 — the former charly/check_cmd.go's checkLivePod,
// deleted) and calls kit.MergeDeployDescriptions directly.

// MergeDeployDescriptions overlays a deployment node's local `plan:` steps onto
// a label-baked LabelDescriptionSet. A baked step — in ANY section (Candy, Box
// or Deploy) — carrying the same author id as a local step is replaced by that
// local step IN PLACE: the local step executes at the baked step's position in
// the baked step's own section and is not appended a second time. This is the
// E-5 single-identity correction: a bed's session-lifecycle steps (same ids as
// the baked candy steps) now overshadow the baked ones instead of running
// twice under different scoped origins — the duplicate execution that churned
// the shared persisted session file and broke the detached recorder's bracket.
// Remaining local steps (no id match) are appended as one new Deploy-section
// entry ("deploy-local:<origin>"). This is the per-host override surface for
// acceptance steps (charly.yml deploy entries). If localPlan is empty, returns
// baked unchanged.
func MergeDeployDescriptions(baked *LabelDescriptionSet, localPlan []spec.Step, originName string) *LabelDescriptionSet {
	if len(localPlan) == 0 {
		return baked
	}
	if baked == nil {
		baked = &LabelDescriptionSet{}
	}
	// Index baked steps by author id across EVERY section for replace-by-id
	// (only steps carrying an explicit Op.ID participate; derived ids are
	// positional and not stable across an overlay). Later sections may shadow
	// earlier ones on an id collision inside the baked set itself — last wins,
	// matching the previous Deploy-only walk.
	type loc struct {
		ld, st int
		sec    []LabeledDescription // same backing slice as the section field — element writes land in the baked set
	}
	locByID := map[string]loc{}
	sections := []struct {
		name string
		dst  *[]LabeledDescription
	}{{"candy", &baked.Candy}, {"box", &baked.Box}, {"deploy", &baked.Deploy}}
	for _, sec := range sections {
		for li := range *sec.dst {
			for si := range (*sec.dst)[li].Plan {
				if id := (*sec.dst)[li].Plan[si].ID; id != "" {
					locByID[id] = loc{li, si, *sec.dst}
				}
			}
		}
	}
	var fresh []spec.Step
	for _, st := range localPlan {
		if id := st.ID; id != "" {
			if l, ok := locByID[id]; ok {
				// replace by id — in the section the baked step lived in, so the
				// substitution keeps the baked ordering (the E-5 session-create
				// must run FIRST, ahead of the baked appium suite).
				l.sec[l.ld].Plan[l.st] = st
				continue
			}
		}
		fresh = append(fresh, st)
	}
	if len(fresh) > 0 {
		baked.Deploy = append(baked.Deploy, LabeledDescription{
			Origin: "deploy-local:" + originName,
			Plan:   fresh,
		})
	}
	return baked
}
