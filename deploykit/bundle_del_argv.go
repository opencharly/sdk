package deploykit

// BundleDelArgv returns the argv (everything after the charly binary) for a non-interactive
// `charly bundle del <name>`: the verb, the name, and the ONE valid skip-confirmation flag. Every
// programmatic teardown builds its command through this single helper — in-process
// (proclifecycle.RunCharlySubcommand), out-of-process (exec.Command), and the systemd-run TTL
// timer — so the flag can never drift across call sites again (R3 hoist: this was byte-identically
// duplicated in charly core, candy/plugin-bundle, and candy/plugin-substrate).
//
// The flag is `--assume-yes`, NOT `--yes`/`--force`: the command:bundle plugin's `charly bundle del`
// Kong grammar (candy/plugin-bundle) renders its AssumeYes field as --assume-yes because Kong
// derives the long name from the FIELD (the `long:"yes"` tag is a Kong no-op in the separate-tag
// form), with `-y` as the short form. A `--yes`/`--force` drift — neither of which Kong accepts —
// once aborted teardown at arg-parse and silently leaked the resource (see CHANGELOG/); the
// deploy-del-flag regression test guards this.
func BundleDelArgv(name string) []string {
	return []string{"bundle", "del", name, "--assume-yes"}
}
