package bkimage

const (
	DefaultImage         = "moby/buildkit:buildx-stable-1" // TODO: make this verified
	QemuImage            = "tonistiigi/binfmt:latest"      // TODO: make this verified
	DefaultRootlessImage = DefaultImage + "-rootless"
)
