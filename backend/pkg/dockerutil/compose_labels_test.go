package docker

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestComposeLabels(t *testing.T) {
	labels := map[string]string{
		ComposeProjectLabelKey: " myproj ",
		ComposeServiceLabelKey: "web",
	}
	{

		got := ComposeProjectLabel(labels)
		assert.Equal(t, "myproj", got,
			"ComposeProjectLabel() = %q, want %q", got, "myproj")
	}
	{

		got := ComposeServiceLabel(labels)
		assert.Equal(t, "web", got,
			"ComposeServiceLabel() = %q, want %q", got, "web")
	}
	{

		got := ComposeProjectLabel(nil)
		assert.Empty(t, got,
			"ComposeProjectLabel(nil) = %q, want %q", got, "")
	}
	{

		got := ComposeServiceLabel(map[string]string{})
		assert.Empty(t, got,
			"ComposeServiceLabel(empty) = %q, want %q", got, "")
	}

}
