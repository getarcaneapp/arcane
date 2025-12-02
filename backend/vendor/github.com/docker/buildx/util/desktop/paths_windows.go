package desktop

func BuildServerAddr() (string, error) {
	return "npipe:////./pipe/dockerDesktopBuildServer", nil
}
