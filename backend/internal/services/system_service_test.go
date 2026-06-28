package services

import (
	"strings"
	"testing"

	"github.com/getarcaneapp/arcane/types/v2/system"
	"github.com/stretchr/testify/require"
)

func TestSystemServiceConvertToDockerComposeUsesAlignedYAMLFormat(t *testing.T) {
	svc := &SystemService{}

	compose, envVars, serviceName, err := svc.ConvertToDockerCompose(&system.DockerRunCommand{
		Image:       "nginx:1.27-alpine",
		Name:        "web",
		Ports:       []string{"8080:80"},
		Volumes:     []string{"data:/data"},
		Environment: []string{"FOO=bar", "BAZ=qux"},
		Restart:     "unless-stopped",
		Workdir:     "/srv/app",
		User:        "1000:1000",
		Entrypoint:  "/entrypoint.sh",
		Command:     "nginx -g 'daemon off;'",
		Interactive: true,
		TTY:         true,
		Privileged:  true,
		Labels:      []string{"com.example.role=frontend"},
		HealthCheck: "curl -f http://localhost || exit 1",
		MemoryLimit: "512m",
		CPULimit:    "0.5",
	})
	require.NoError(t, err)

	require.Equal(t, "web", serviceName)
	require.Equal(t, "FOO=bar\nBAZ=qux", envVars)
	require.Equal(t, strings.TrimPrefix(`
services:
    web:
        image: nginx:1.27-alpine
        container_name: web
        ports:
            - 8080:80
        volumes:
            - data:/data
        environment:
            - FOO=bar
            - BAZ=qux
        restart: unless-stopped
        working_dir: /srv/app
        user: 1000:1000
        entrypoint: /entrypoint.sh
        command: nginx -g 'daemon off;'
        stdin_open: true
        tty: true
        privileged: true
        labels:
            - com.example.role=frontend
        healthcheck:
            test: curl -f http://localhost || exit 1
        deploy:
            resources:
                limits:
                    memory: 512m
                    cpus: "0.5"
`, "\n"), compose)
}
