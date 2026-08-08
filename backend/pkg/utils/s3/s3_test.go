package s3

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestConfigurationValidateRequiresRegionOnlyForAWS(t *testing.T) {
	configuration := Configuration{Name: "Offsite", Bucket: "backups", AccessKeyID: "access-key", SecretAccessKey: "secret-key"}
	require.EqualError(t, configuration.Validate(true), "region is required for AWS S3")

	configuration.Endpoint = "s3.example.com"
	require.NoError(t, configuration.Validate(true))
}

func TestValidateEndpoint(t *testing.T) {
	require.NoError(t, ValidateEndpoint(""))
	require.NoError(t, ValidateEndpoint("minio.internal:9000"))
	require.NoError(t, ValidateEndpoint("http://10.0.0.5:9000"))
	require.NoError(t, ValidateEndpoint("https://s3.example.com"))

	require.ErrorContains(t, ValidateEndpoint("ftp://s3.example.com"), "only http and https")
	require.ErrorContains(t, ValidateEndpoint("https://user:pass@s3.example.com"), "credentials in the URL")
	require.ErrorContains(t, ValidateEndpoint("https://s3.example.com?x=1"), "query strings and fragments")
	require.ErrorContains(t, ValidateEndpoint("https://s3.example.com#frag"), "query strings and fragments")
	require.ErrorContains(t, ValidateEndpoint("https://"), "host is required")
}

func TestConfigurationRusticEnvironment(t *testing.T) {
	configuration := Configuration{
		Endpoint:        "http://s3.example.com/",
		Bucket:          "backups",
		AccessKeyID:     "access-key",
		SecretAccessKey: "secret-key",
		Prefix:          "/production/",
		UseSSL:          true,
		ForcePathStyle:  false,
	}

	require.Equal(t, []string{
		"RUSTIC_REPOSITORY=opendal:s3",
		"RUSTIC_REPO_OPT_BUCKET=backups",
		"RUSTIC_REPO_OPT_ROOT=/production/arcane-volume-backups/instance-1",
		"AWS_ACCESS_KEY_ID=access-key",
		"AWS_SECRET_ACCESS_KEY=secret-key",
		"AWS_EC2_METADATA_DISABLED=true",
		"RUSTIC_REPO_OPT_ENDPOINT=https://s3.example.com",
		"RUSTIC_REPO_OPT_REGION=auto",
		"AWS_REGION=auto",
		"RUSTIC_REPO_OPT_ENABLE_VIRTUAL_HOST_STYLE=true",
	}, configuration.RusticEnvironment("arcane-volume-backups", "instance-1"))
}
