package driver

type Feature string

const OCIExporter Feature = "OCI exporter"
const DockerExporter Feature = "Docker exporter"

const CacheExport Feature = "Cache export"
const MultiPlatform Feature = "Multi-platform build"
const DirectPush Feature = "Direct push"

const DefaultLoad Feature = "Automatically load images to the Docker Engine image store"
