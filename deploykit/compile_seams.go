package deploykit

// compile_seams.go — seams the deploy-plan compiler (this package) reaches back into
// charly through, for kernel machinery that stays in charly: the provider registry
// (act-op → typed/external step resolution). charly injects the real impl at init.

// CompileActOp lowers a `run:` act op (install verb or `plugin:` verb) into an
// InstallStep. Provider-coupled (kernel provider registry), so charly owns it and
// injects it; the compiler calls the seam.
var CompileActOp func(op *Op, layer CandyModel, img *ResolvedBox) InstallStep

// CompileServiceSteps lowers a candy's service: list into install steps. Loader- and
// service-render-coupled (LoadBuildConfigForBox + RenderService live in charly), so
// charly owns it and injects it.
var CompileServiceSteps func(layer CandyModel, img *ResolvedBox, hostCtx HostContext) []InstallStep
