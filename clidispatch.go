package sdk

import "github.com/alecthomas/kong"

// clidispatch.go — the ONE shared in-process CLI-dispatch helper every COMPILED-IN command
// plugin uses to kong-parse its pass-through args WITHOUT letting kong terminate charly's
// process (a compiled-in command runs in charly's OWN process via Invoke(OpRun), so a kong
// os.Exit would kill charly whole). It replaces the per-plugin
//
//	kong.New(&cli, kong.Name(name), kong.Exit(func(int){}))
//
// boilerplate that every command plugin copied (R3: one helper, every plugin). The no-op
// kong.Exit was a latent bug: kong's `--help`/`--version` flags PRINT and then call the
// configured Exit; a no-op Exit returned control to Parse, which then either ran the selected
// leaf with empty args (a subcommand `--help` → the command executed on no input → a non-zero
// exit) or fell through to a spurious `expected "<subcommand>"` parse error (a top-level
// `--help`). The panic-sentinel below unwinds Parse the instant kong calls Exit — BEFORE either
// wrong path — so `--help`/`--version` at ANY level cleanly stop (exit 0, no leaf run), a
// kong-requested non-zero exit becomes *ExitCodeError (honored by the host's exit-code mapping),
// and a genuine parse error propagates unchanged. Proven across the full matrix in
// clidispatch_test.go.

// kongExitPanic carries a kong-requested process exit code across the panic that unwinds
// kong.Parse. It is recovered ONLY inside parseInProc; any other panic re-panics.
type kongExitPanic struct{ code int }

// parseInProc is the shared primitive: build the kong grammar for cli, parse args, and recover
// kong's Exit (help/version/version-error) as a clean stop. It returns the parsed *kong.Context
// (nil when kong exited or on a parse error), done=true when kong handled the invocation itself
// (--help/--version printed), and any error. A non-zero kong exit code surfaces as *ExitCodeError.
func parseInProc(name string, cli any, args []string, opts []kong.Option) (kctx *kong.Context, done bool, err error) {
	defer func() {
		if r := recover(); r != nil {
			ke, ok := r.(kongExitPanic)
			if !ok {
				panic(r) // not ours — propagate
			}
			kctx, done = nil, true
			if ke.code != 0 {
				err = &ExitCodeError{Code: ke.code}
			} else {
				err = nil
			}
		}
	}()
	base := []kong.Option{kong.Name(name), kong.Exit(func(code int) { panic(kongExitPanic{code}) })}
	parser, perr := kong.New(cli, append(base, opts...)...)
	if perr != nil {
		return nil, false, perr
	}
	kctx, perr = parser.Parse(args)
	if perr != nil {
		return nil, false, perr
	}
	return kctx, false, nil
}

// RunInProcCLI parses args into the kong grammar cli and runs the selected leaf's Run() — the
// entry for a command plugin whose grammar carries Run() leaves (the common case). On
// `--help`/`--version` it prints and returns nil WITHOUT running any leaf; a non-zero kong exit
// becomes *ExitCodeError; a parse error propagates. Pass any extra kong.Option (kong.Description,
// kong.Bind, …) via opts — kong.Name + the exit-sentinel are supplied for you.
func RunInProcCLI(name string, cli any, args []string, opts ...kong.Option) error {
	kctx, done, err := parseInProc(name, cli, args, opts)
	if err != nil || done {
		return err
	}
	return kctx.Run()
}

// ParseInProcCLI parses args into cli WITHOUT running any leaf — the entry for a command plugin
// that dispatches MANUALLY after parsing (it reads the populated struct itself rather than
// relying on kong's Run()). It returns done=true when kong printed --help/--version: the caller
// MUST return nil immediately and NOT proceed to its post-parse logic (otherwise `charly <cmd>
// --help` would run the command's action on default flags). A parse error is returned as err.
func ParseInProcCLI(name string, cli any, args []string, opts ...kong.Option) (done bool, err error) {
	_, done, err = parseInProc(name, cli, args, opts)
	return done, err
}
