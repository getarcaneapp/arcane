package middleware

import (
	"testing"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humagin"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type testOperationProvider struct {
	operation *huma.Operation
}

func (p testOperationProvider) Operation() *huma.Operation {
	return p.operation
}

func TestParseSecurityRequirements(t *testing.T) {
	gin.SetMode(gin.TestMode)

	router := gin.New()
	apiGroup := router.Group("/api")
	humaConfig := huma.DefaultConfig("test", "1.0.0")
	humaConfig.Security = []map[string][]string{
		{"BearerAuth": {}},
		{"ApiKeyAuth": {}},
	}
	api := humagin.NewWithGroup(router, apiGroup, humaConfig)

	testCases := []struct {
		name     string
		security []map[string][]string
		expected securityRequirements
	}{
		{
			name:     "nil operation security inherits top-level auth",
			security: nil,
			expected: securityRequirements{
				isRequired: true,
				bearerAuth: true,
				apiKeyAuth: true,
			},
		},
		{
			name:     "explicit empty security stays public",
			security: []map[string][]string{},
			expected: securityRequirements{},
		},
		{
			name: "explicit dual auth stays protected",
			security: []map[string][]string{
				{"BearerAuth": {}},
				{"ApiKeyAuth": {}},
			},
			expected: securityRequirements{
				isRequired: true,
				bearerAuth: true,
				apiKeyAuth: true,
			},
		},
		{
			name: "explicit api key auth stays api-key-only",
			security: []map[string][]string{
				{"ApiKeyAuth": {}},
			},
			expected: securityRequirements{
				isRequired: true,
				apiKeyAuth: true,
			},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			require.Equal(t, testCase.expected, parseSecurityRequirementsInternal(api, testOperationProvider{
				operation: &huma.Operation{Security: testCase.security},
			}))
		})
	}
}
