package kit

import "github.com/opencharly/sdk/spec"

// description_merge.go — P12a follow-up: MergeDeployDescriptions relocated from
// charly/description_collect.go. Pure over LabelDescriptionSet/LabeledDescription/spec.Step
// (already kit-native types, see planrun.go) — zero core state. Its sole caller, the "live"
// gather engine's pod path, is plugin-side too (candy/plugin-check/live_gather.go's
// pluginCheckLivePod, K1-unblock wave arm 1 — the former charly/check_cmd.go's checkLivePod,
// deleted) and calls kit.MergeDeployDescriptions directly.

// MergeDeployDescriptions overlays a deployment node's local `plan:` steps onto
// a label-baked LabelDescriptionSet's Deploy section. A baked deploy step with
// the same step id is replaced by the local one; otherwise the local step is
// appended. This is the per-host override surface for acceptance steps
// (charly.yml deploy entries). If localPlan is empty, returns baked unchanged.
func MergeDeployDescriptions(baked *LabelDescriptionSet, localPlan []spec.Step, originName string) *LabelDescriptionSet {
	if len(localPlan) == 0 {
		return baked
	}
	if baked == nil {
		baked = &LabelDescriptionSet{}
	}
	// Index baked deploy steps by author id for replace-by-id (only steps
	// carrying an explicit Op.ID participate; derived ids are positional and
	// not stable across an overlay).
	type loc struct{ ld, st int }
	locByID := map[string]loc{}
	for li := range baked.Deploy {
		for si := range baked.Deploy[li].Plan {
			if id := baked.Deploy[li].Plan[si].ID; id != "" {
				locByID[id] = loc{li, si}
			}
		}
	}
	var fresh []spec.Step
	for _, st := range localPlan {
		if id := st.ID; id != "" {
			if l, ok := locByID[id]; ok {
				baked.Deploy[l.ld].Plan[l.st] = st // replace by id
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
