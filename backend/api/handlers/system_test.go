package handlers

import (
	"context"
	"strings"
	"testing"

	"github.com/getarcaneapp/arcane/backend/v2/internal/services"
	"github.com/getarcaneapp/arcane/types/v2/system"
	"github.com/stretchr/testify/require"
)

func TestSystemHandlerConvertDockerRunUsesAlignedYAMLFormat(t *testing.T) {
	handler := &SystemHandler{systemService: &services.SystemService{}}

	output, err := handler.ConvertDockerRun(context.Background(), &ConvertDockerRunInput{
		Body: system.ConvertDockerRunRequest{
			DockerRunCommand: "docker run --name web -p 8080:80 -v data:/data -e FOO=bar -e BAZ=qux " +
				"--restart unless-stopped -w /srv/app -u 1000:1000 --entrypoint /entrypoint.sh " +
				"-it --privileged --label com.example.role=frontend " +
				"--health-cmd 'curl -f http://localhost || exit 1' -m 512m --cpus 0.5 " +
				"nginx:1.27-alpine nginx -g 'daemon off;'",
		},
	})
	require.NoError(t, err)

	require.True(t, output.Body.Success)
	require.Equal(t, "web", output.Body.ServiceName)
	require.Equal(t, "FOO=bar\nBAZ=qux", output.Body.EnvVars)
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
volumes:
    data:
        external: true
`, "\n"), output.Body.DockerCompose)
}

func TestSystemHandlerConvertDockerRunSupportsEnvFileAndUlimit(t *testing.T) {
	handler := &SystemHandler{systemService: &services.SystemService{}}

	output, err := handler.ConvertDockerRun(context.Background(), &ConvertDockerRunInput{
		Body: system.ConvertDockerRunRequest{
			DockerRunCommand: "docker run --name worker --env-file .env --ulimit nofile=1024:2048 alpine:3.20 sleep 30",
		},
	})
	require.NoError(t, err)

	require.Equal(t, "worker", output.Body.ServiceName)
	require.Empty(t, output.Body.EnvVars)
	require.Contains(t, output.Body.DockerCompose, "env_file:\n            - .env")
	require.Contains(t, output.Body.DockerCompose, "ulimits:\n            nofile:\n                soft: 1024\n                hard: 2048")
}
