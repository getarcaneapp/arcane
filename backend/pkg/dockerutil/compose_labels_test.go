package docker

import "testing"

func TestComposeLabels(t *testing.T) {
	labels := map[string]string{
		ComposeProjectLabelKey: " myproj ",
		ComposeServiceLabelKey: "web",
	}

	if got := ComposeProjectLabel(labels); got != "myproj" {
		t.Errorf("ComposeProjectLabel() = %q, want %q", got, "myproj")
	}
	if got := ComposeServiceLabel(labels); got != "web" {
		t.Errorf("ComposeServiceLabel() = %q, want %q", got, "web")
	}

	if got := ComposeProjectLabel(nil); got != "" {
		t.Errorf("ComposeProjectLabel(nil) = %q, want %q", got, "")
	}
	if got := ComposeServiceLabel(map[string]string{}); got != "" {
		t.Errorf("ComposeServiceLabel(empty) = %q, want %q", got, "")
	}
}
