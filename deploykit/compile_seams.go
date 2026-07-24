package deploykit

// compile_seams.go — seams the deploy-plan compiler (this package) reaches back into
// charly through, for kernel machinery that stays in charly: the provider registry
// (act-op → typed/external step resolution). charly injects the real impl at init.
//
// K5-A item 1 (compile-seam ctx-threading) retired the CompileActOp core-init()-set
// func var this file used to declare: it was a hidden, implicit dependency on
// charly-core's init() having run in the SAME process — invisible in
// BuildDeployPlan's own signature, and silently nil (a panic waiting to happen) for
// any caller that didn't happen to share that process. The replacement,
// compile_construct_step.go's constructOpStep, takes an EXPLICIT ctx/exec pair
// (BuildDeployPlan's own new parameters) and reaches the provider registry over the
// "construct-step" HostBuild seam ONLY for the rare `run: plugin: <word>` case — the
// common install-verb case never touches the wire at all (buildGenericOpStep, fully
// portable, no core-only dependency). See compile_construct_step.go.

// CompileServiceSteps lowers a candy's service: list into install steps. Loader- and
// service-render-coupled (LoadBuildConfigForBox + RenderService live in charly), so
// charly owns it and injects it.
var CompileServiceSteps func(layer CandyModel, img *ResolvedBox, hostCtx HostContext) []InstallStep
