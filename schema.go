package sdk

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"strings"

	"cuelang.org/go/cue/cuecontext"

	"github.com/opencharly/spec/capability"
	"github.com/opencharly/spec/climodel"
	pb "github.com/opencharly/spec/proto"
	"github.com/opencharly/spec/schemaconcat"
	"github.com/opencharly/spec/spec"
)

// ProvidedCapability is one capability a plugin serves plus the CUE def that
// validates its plugin_input — the SDK-facing form of the proto ProvidedCapability.
// An external plugin lists these in its Describe; the host validates authored
// plugin_input for each word against its def in the served schema.
//
// Relocated to spec/capability (#55 import-purity, Rule 2: the descriptor types are
// plain Go structs whose only non-stdlib reference is spec/spec, so a cuelang-free
// slice holds them — keeping cuelang confined to spec/climodel's ValidateGenerated);
// re-exported here so the 92 plugin candy call sites compile UNCHANGED. charly core
// re-points to capability.ProvidedCapability directly (the import-purity gate).
type ProvidedCapability = capability.ProvidedCapability

// CLISubcommand is one DECLARED child of a class="command" capability's own CLI word — see
// ProvidedCapability.Subcommands. Relocated to spec/climodel (#55 import-purity); re-exported
// here so candy call sites compile UNCHANGED.
type CLISubcommand = climodel.CLISubcommand

// StepContract is the SDK-facing form of the proto StepContract — a class="step" plugin's
// declared install-step Scope/Venue/Gate. Reverse is NOT declared (an external step's
// teardown ops are recorded dynamically from its OpExecute reply).
//
// The typed spec.StepContract is the single canonical form (the former string-form
// StepContract was deleted in the strict-cleanup cutover — R3); re-exported here so
// plugin candy call sites compile UNCHANGED.
type StepContract = spec.StepContract

// BuildCapabilities is the serve-side half of the "every plugin ships its own CUE
// schema" contract. It concatenates the plugin's embedded schema/*.cue via the SAME
// schemaconcat contract charly uses for its base (R3 — one concat loop, no
// duplicate), compiles it STANDALONE to fail loudly on a broken or empty schema
// (a self-contained schema must compile alone — the same property that lets
// `cue exp gengotypes` generate the plugin's Go params), and assembles the Describe
// reply carrying the raw .cue source the host splices onto its base.
//
// schemaFS is the plugin's `//go:embed schema/*.cue` FS; dir is the embedded
// subdirectory ("schema"). Both the SDK and charly's base reach the same internal
// schemaconcat because the SDK lives under charly/ — an external module imports
// only this SDK, never charly/internal directly.
func BuildCapabilities(calver string, provided []ProvidedCapability, schemaFS fs.FS, dir string) (*pb.Capabilities, error) {
	// Stub-gate relaxation (schema-compaction cutover): an INPUT-LESS plugin (no
	// capability declares an InputDef) may ship no schema at all — pass a nil
	// schemaFS. A plugin that declares an input def must serve the schema
	// defining it (the host cross-checks def + primary at registration).
	var body string
	if schemaFS != nil {
		var err error
		body, _, err = schemaconcat.ConcatSchema(schemaFS, dir, nil)
		if err != nil {
			return nil, fmt.Errorf("plugin schema: %w", err)
		}
	}
	hasInputDef := false
	for _, c := range provided {
		if c.InputDef != "" {
			hasInputDef = true
		}
	}
	if strings.TrimSpace(body) == "" {
		if hasInputDef {
			return nil, fmt.Errorf("plugin declares an input def but ships no CUE schema")
		}
		body = ""
	} else if v := cuecontext.New().CompileString(body); v.Err() != nil {
		return nil, fmt.Errorf("plugin schema does not compile: %w", v.Err())
	}
	out := make([]*pb.ProvidedCapability, 0, len(provided))
	for _, c := range provided {
		pc := &pb.ProvidedCapability{Class: c.Class, Word: c.Word, InputDef: c.InputDef, Structural: c.Structural, Lifecycle: c.Lifecycle, Preresolve: c.Preresolve, Validates: c.Validates, Phase: c.Phase, Primary: c.Primary}
		if c.CommandModel != nil {
			if c.Class != "command" {
				return nil, fmt.Errorf("plugin capability %s:%s declares a command model outside class=command", c.Class, c.Word)
			}
			if err := ValidateGenerated("#CLIModel", c.CommandModel); err != nil {
				return nil, fmt.Errorf("plugin command model %s: %w", c.Word, err)
			}
			model, err := json.Marshal(c.CommandModel)
			if err != nil {
				return nil, fmt.Errorf("plugin command model %s: %w", c.Word, err)
			}
			pc.CommandModelJson = model
		}
		if c.StepContract != nil {
			pc.StepContract = &pb.StepContract{Scope: c.StepContract.Scope.String(), Venue: int32(c.StepContract.Venue), Gate: string(c.StepContract.Gate), Emits: c.StepContract.Emits}
		}
		for _, sc := range c.Subcommands {
			pc.Subcommands = append(pc.Subcommands, &pb.CLISubcommand{Name: sc.Name, Help: sc.Help, Hidden: sc.Hidden})
		}
		if c.DeployTraits != nil {
			pc.DeployTraits = &pb.DeployTraits{
				Venue:                c.DeployTraits.Venue,
				ImageBacked:          c.DeployTraits.ImageBacked,
				ImageContext:         c.DeployTraits.ImageContext,
				MachineVenue:         c.DeployTraits.MachineVenue,
				ExclusiveVenue:       c.DeployTraits.ExclusiveVenue,
				LeafOnly:             c.DeployTraits.LeafOnly,
				BracketedLifecycle:   c.DeployTraits.BracketedLifecycle,
				BedTarget:            c.DeployTraits.BedTarget,
				SupportsEphemeral:    c.DeployTraits.SupportsEphemeral,
				SupportsFromSnapshot: c.DeployTraits.SupportsFromSnapshot,
			}
		}
		out = append(out, pc)
	}
	return &pb.Capabilities{
		Calver:          calver,
		ProtocolVersion: ProtocolVersion,
		Provided:        out,
		SchemaCue:       body,
	}, nil
}
