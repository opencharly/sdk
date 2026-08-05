package kit

// container_image.go — re-export of the container→image-ref inspector, RELOCATED to the
// spec/container fabric slice github.com/opencharly/spec/container/image_inspect.go (#55
// CHECK-ENGINE cone Option A — a genuinely pure podman/docker CLI invocation importing zero kit).
// THE single container→image-ref inspector — there is exactly one inspect implementation. kit
// re-exports the symbols here so every existing kit.ContainerImageRef / kit.ContainerImage call
// site (charly core + candy/plugin-deploy-pod) is untouched. The former core caller,
// check_endpoint_resolve.go's resolveImageLabelFor, relocated to candy/plugin-check's
// resolve_endpoint.go (#55 W3 B7), which imports spec/container directly, like every other new
// consumer should.

import "github.com/opencharly/spec/container"

// ContainerImageRef returns the image ref backing a running container (.Config.Image via
// `<engine> inspect`). Re-exported from container.ContainerImageRef (the body lives there).
var ContainerImageRef = container.ContainerImageRef

// ContainerImage returns the image ref for a running container, best-effort ("" on error).
// Re-exported from container.ContainerImage.
var ContainerImage = container.ContainerImage
