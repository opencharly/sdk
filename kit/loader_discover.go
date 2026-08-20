package kit

// loader_discover.go — re-export shims for the kind-blind config-loader
// discover-walk LEAF PRIMITIVES, relocated to sdk/spec (FLOOR-SLIM axis-A
// mechanical batch, zero logic change — the sibling of node_helpers.go's
// ClassifyDoc relocation) so charly core's prescan (plugin_prescan.go, the
// loader-walk Boundary seam — a core M prescan-dispatch) can call them without
// importing a mechanism kit. Aliased here so every existing kit.FindEntityDirs
// / kit.DiscoverSkipDir / kit.DirExists / kit.FileExists call site
// (sdk/loaderkit's discover.go/walk.go, sdk/deploykit, candy/plugin-box,
// sdk/kit/scaffold.go) keeps compiling unchanged (R3 — ONE copy, in spec now).

import "github.com/opencharly/spec/loader"

// FindEntityDirs walks a scan root and returns every directory that contains
// the given canonical filename. See loader.FindEntityDirs.
var FindEntityDirs = loader.FindEntityDirs

// DiscoverSkipDir reports whether a directory name is a VCS or build-artifact
// dir that never contains a discoverable charly.yml manifest. See loader.DiscoverSkipDir.
var DiscoverSkipDir = loader.DiscoverSkipDir

// DirExists reports whether path exists and is a directory. See loader.DirExists.
var DirExists = loader.DirExists

// FileExists reports whether path exists and is a regular (non-dir) file.
// See loader.FileExists.
var FileExists = loader.FileExists
