// parse_doc_stream.go — the ONE per-document pipeline over an in-memory multi-document YAML
// stream (parser consolidation F2.5). The FILE walk (walk.parseDocs) and the binary-embedded
// default-vocabulary stream (the host's materializeDocStream) both drove the SAME per-document
// classify → #NodeDoc gate → registered DocParser → deterministic directive serialization →
// import/discover collection loop, hand-rolled twice. This is that loop, once: ParseDocStream
// returns every parsed node-form document (the wire-safe spec.LoadedDoc shape the walk appends
// onto its spec.LoadedProject and the embedded stream replays materialize+merge over), plus the
// concatenated flat-import queue and the anchored discover scan-specs — all three consumed by the
// file walk, the docs alone by an embedded stream.
package loaderkit

import (
	"bytes"
	"fmt"
	"sort"

	"github.com/opencharly/sdk/kit"
	"github.com/opencharly/spec/spec"
	"gopkg.in/yaml.v3"
)

// ParseDocStream parses `data` (a multi-document YAML stream) into its node-form documents via
// the ONE shared per-document pipeline: kit.ClassifyDoc (skip kit.DocShapeEmpty), the host's
// #NodeDoc validate-before-execute gate (seams.GateDoc), then the registered spec.DocParser
// (seams.Parser) — collecting each parsed doc's reserved directives (deterministically
// serialized, exactly the shape the host re-decodes for MaterializeLoadedProject), the doc's
// flat `import:` queue and ANCHORED `discover:` scan-specs. srcLabel labels diagnostics; srcDir
// anchors relative discover paths.
func ParseDocStream(data []byte, srcLabel, srcDir string, seams spec.WalkSeams) ([]spec.LoadedDoc, spec.ImportList, []spec.ScanSpec, error) {
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	docIdx := 0
	var docs []spec.LoadedDoc
	var importQueue spec.ImportList
	var scanSpecs []spec.ScanSpec
	for {
		var node yaml.Node
		if err := decoder.Decode(&node); err != nil {
			if err.Error() == "EOF" {
				break
			}
			return nil, nil, nil, fmt.Errorf("%s:doc%d: %w", srcLabel, docIdx, err)
		}
		shape, err := kit.ClassifyDoc(&node)
		if err != nil {
			return nil, nil, nil, fmt.Errorf("%s:doc%d: %w", srcLabel, docIdx, err)
		}
		switch shape {
		case kit.DocShapeNode:
			label := fmt.Sprintf("%s:doc%d", srcLabel, docIdx)
			// VALIDATE-BEFORE-EXECUTE: the whole node-form document against the host's #NodeDoc
			// gate (strict + closed) BEFORE anything is parsed further.
			raw, err := yaml.Marshal(&node)
			if err != nil {
				return nil, nil, nil, fmt.Errorf("%s: re-marshal node-form doc: %w", label, err)
			}
			if err := seams.GateDoc(label, raw); err != nil {
				return nil, nil, nil, err
			}
			// Parse the document into its reserved directives + the generic spec.ParsedProject via
			// the registered config front-end (spec.DocParser). The host threads the
			// registry-derived kind-recognition DATA (Threaded); the parse itself never touches
			// the registry.
			directives, pp, err := seams.Parser.ParseDoc(&node, seams.Threaded())
			if err != nil {
				return nil, nil, nil, fmt.Errorf("%s: %w", label, err)
			}
			doc := spec.LoadedDoc{Project: pp, SrcDir: srcDir, SrcLabel: label}
			if len(directives) > 0 {
				// The raw reserved-directive mapping (version/repo/defaults/provides/discover;
				// import is carried too so the host can re-decode it, e.g. for diagnostics) — key
				// order via sorted keys for determinism.
				keys := make([]string, 0, len(directives))
				for k := range directives {
					keys = append(keys, k)
				}
				sort.Strings(keys)
				dirMap := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
				for _, k := range keys {
					dirMap.Content = append(dirMap.Content, kit.ScalarNode(k), directives[k])
				}
				// Serialize the directive mapping as YAML bytes (NOT JSON) so the host replays the
				// original mergeUnifiedDocs decode exactly — yaml.Unmarshal(directives, &sub) honors
				// the custom YAML unmarshalers on import/discover, which a JSON body would break.
				body, merr := yaml.Marshal(dirMap)
				if merr != nil {
					return nil, nil, nil, fmt.Errorf("%s: directives: %w", label, merr)
				}
				doc.Directives = body
				if impNode, ok := directives["import"]; ok {
					var il spec.ImportList
					if derr := impNode.Decode(&il); derr != nil {
						return nil, nil, nil, fmt.Errorf("%s: decoding import: %w", label, derr)
					}
					importQueue = append(importQueue, il...)
				}
				if discNode, ok := directives["discover"]; ok {
					var dc spec.DiscoverConfig
					if derr := discNode.Decode(&dc); derr != nil {
						return nil, nil, nil, fmt.Errorf("%s: decoding discover: %w", label, derr)
					}
					scanSpecs = append(scanSpecs, kit.AnchorScanSpecs(dc, srcDir)...)
				}
			}
			docs = append(docs, doc)
		case kit.DocShapeEmpty:
			// Skip empty docs (YAML streams commonly end with "---\n").
		}
		docIdx++
	}
	return docs, importQueue, scanSpecs, nil
}
