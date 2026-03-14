package project

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestDetailsUnmarshalJSON_AcceptsUnitBytesMemLimitFormats(t *testing.T) {
	tests := []struct {
		name        string
		memLimitRaw string
		expected    int64
	}{
		{
			name:        "human-readable string",
			memLimitRaw: `"256m"`,
			expected:    268435456,
		},
		{
			name:        "quoted byte string",
			memLimitRaw: `"268435456"`,
			expected:    268435456,
		},
		{
			name:        "numeric bytes",
			memLimitRaw: `268435456`,
			expected:    268435456,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			payload := `{
				"id": "project-1",
				"name": "test-project",
				"path": "/tmp/test-project",
				"status": "running",
				"serviceCount": 1,
				"runningCount": 1,
				"createdAt": "2026-03-13T00:00:00Z",
				"updatedAt": "2026-03-13T00:00:00Z",
				"services": [
					{
						"name": "web",
						"image": "nginx:latest",
						"mem_limit": ` + tt.memLimitRaw + `
					}
				],
				"runtimeServices": [
					{
						"name": "web",
						"image": "nginx:latest",
						"status": "running",
						"serviceConfig": {
							"name": "web",
							"image": "nginx:latest",
							"mem_limit": ` + tt.memLimitRaw + `
						}
					}
				]
			}`

			var details Details
			if err := json.Unmarshal([]byte(payload), &details); err != nil {
				t.Fatalf("unmarshal details: %v", err)
			}

			if len(details.Services) != 1 {
				t.Fatalf("expected 1 service, got %d", len(details.Services))
			}
			if got := int64(details.Services[0].MemLimit); got != tt.expected {
				t.Fatalf("expected services[0].MemLimit=%d, got %d", tt.expected, got)
			}

			if len(details.RuntimeServices) != 1 {
				t.Fatalf("expected 1 runtime service, got %d", len(details.RuntimeServices))
			}
			if details.RuntimeServices[0].ServiceConfig == nil {
				t.Fatal("expected runtime service config to be decoded")
			}
			if got := int64(details.RuntimeServices[0].ServiceConfig.MemLimit); got != tt.expected {
				t.Fatalf("expected runtimeServices[0].serviceConfig.MemLimit=%d, got %d", tt.expected, got)
			}
		})
	}
}

func TestDetailsUnmarshalJSON_NormalizesNestedUnitBytesFields(t *testing.T) {
	payload := `{
		"id": "project-1",
		"name": "test-project",
		"path": "/tmp/test-project",
		"status": "running",
		"serviceCount": 1,
		"runningCount": 1,
		"createdAt": "2026-03-13T00:00:00Z",
		"updatedAt": "2026-03-13T00:00:00Z",
		"services": [
			{
				"name": "web",
				"image": "nginx:latest",
				"build": {
					"context": ".",
					"shm_size": "64m"
				},
				"deploy": {
					"resources": {
						"limits": {
							"memory": "512m"
						},
						"reservations": {
							"memory": "128m"
						}
					}
				}
			}
		]
	}`

	var details Details
	if err := json.Unmarshal([]byte(payload), &details); err != nil {
		t.Fatalf("unmarshal details: %v", err)
	}

	if len(details.Services) != 1 {
		t.Fatalf("expected 1 service, got %d", len(details.Services))
	}

	service := details.Services[0]
	if service.Build == nil {
		t.Fatal("expected build config to be decoded")
	}
	if got := int64(service.Build.ShmSize); got != 67108864 {
		t.Fatalf("expected build.shm_size=67108864, got %d", got)
	}

	if service.Deploy == nil || service.Deploy.Resources.Limits == nil || service.Deploy.Resources.Reservations == nil {
		t.Fatal("expected deploy resource limits and reservations to be decoded")
	}
	if got := int64(service.Deploy.Resources.Limits.MemoryBytes); got != 536870912 {
		t.Fatalf("expected deploy.resources.limits.memory=536870912, got %d", got)
	}
	if got := int64(service.Deploy.Resources.Reservations.MemoryBytes); got != 134217728 {
		t.Fatalf("expected deploy.resources.reservations.memory=134217728, got %d", got)
	}
}

func TestDetailsUnmarshalJSON_InvalidUnitBytesValueReturnsError(t *testing.T) {
	payload := `{
		"id": "project-1",
		"name": "test-project",
		"path": "/tmp/test-project",
		"status": "running",
		"serviceCount": 1,
		"runningCount": 1,
		"createdAt": "2026-03-13T00:00:00Z",
		"updatedAt": "2026-03-13T00:00:00Z",
		"services": [
			{
				"name": "web",
				"image": "nginx:latest",
				"mem_limit": "definitely-not-valid"
			}
		]
	}`

	err := json.Unmarshal([]byte(payload), &Details{})
	if err == nil {
		t.Fatal("expected invalid mem_limit to fail decoding")
	}
	if !strings.Contains(err.Error(), "mem_limit") {
		t.Fatalf("expected error to reference mem_limit, got %v", err)
	}
}
