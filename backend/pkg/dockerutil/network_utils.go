package docker

func IsDefaultNetwork(name string) bool {
	switch name {
	case "bridge", "host", "none", "ingress":
		return true
	default:
		return false
	}
}
