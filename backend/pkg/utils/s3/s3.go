// Package s3 provides reusable S3 configuration and connectivity helpers. It
// deals in infrastructure configuration only; mapping from API DTOs belongs to
// the backup destination domain.
package s3

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/url"
	"path"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	awss3 "github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/google/uuid"
)

const connectionTestPayload = "arcane-s3-connection-test"

// Configuration contains the credentials and addressing options for an S3 destination.
type Configuration struct {
	ID              string
	Name            string
	Endpoint        string
	Bucket          string
	Region          string
	AccessKeyID     string
	SecretAccessKey string
	Prefix          string
	UseSSL          bool
	ForcePathStyle  bool
}

// Normalized returns a copy with surrounding whitespace and prefix separators removed.
func (c Configuration) Normalized() Configuration {
	c.ID = strings.TrimSpace(c.ID)
	c.Name = strings.TrimSpace(c.Name)
	c.Endpoint = strings.TrimSpace(c.Endpoint)
	c.Bucket = strings.TrimSpace(c.Bucket)
	c.Region = strings.TrimSpace(c.Region)
	c.AccessKeyID = strings.TrimSpace(c.AccessKeyID)
	c.SecretAccessKey = strings.TrimSpace(c.SecretAccessKey)
	c.Prefix = strings.Trim(strings.TrimSpace(c.Prefix), "/")
	return c
}

// ValidateEndpoint checks a custom endpoint: only HTTP or HTTPS with a host,
// no userinfo, query, or fragment. Private and self-hosted hosts are allowed.
func ValidateEndpoint(endpoint string) error {
	endpoint = strings.TrimSpace(endpoint)
	if endpoint == "" {
		return nil
	}
	candidate := endpoint
	if !strings.Contains(candidate, "://") {
		candidate = "https://" + candidate
	}
	parsed, err := url.Parse(candidate)
	if err != nil {
		return fmt.Errorf("invalid S3 endpoint: %w", err)
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return errors.New("invalid S3 endpoint: only http and https are supported")
	}
	if parsed.Host == "" {
		return errors.New("invalid S3 endpoint: host is required")
	}
	if parsed.User != nil {
		return errors.New("invalid S3 endpoint: credentials in the URL are not allowed")
	}
	if parsed.RawQuery != "" || parsed.Fragment != "" || parsed.RawFragment != "" {
		return errors.New("invalid S3 endpoint: query strings and fragments are not allowed")
	}
	return nil
}

// Validate verifies the fields required to connect to the configured destination.
func (c Configuration) Validate(requireSecret bool) error {
	c = c.Normalized()
	if c.Name == "" {
		return errors.New("name is required")
	}
	if c.Bucket == "" {
		return errors.New("bucket is required")
	}
	if c.Endpoint == "" && c.Region == "" {
		return errors.New("region is required for AWS S3")
	}
	if err := ValidateEndpoint(c.Endpoint); err != nil {
		return err
	}
	if c.AccessKeyID == "" {
		return errors.New("access key ID is required")
	}
	if requireSecret && c.SecretAccessKey == "" {
		return errors.New("secret access key is required")
	}
	return nil
}

// ConnectionFieldsEqual reports whether two configurations address the same
// service the same way. A change in any of these fields means a stored secret
// may no longer be combined with the configuration.
func ConnectionFieldsEqual(a, b Configuration) bool {
	a, b = a.Normalized(), b.Normalized()
	return a.Endpoint == b.Endpoint &&
		a.Bucket == b.Bucket &&
		a.Region == b.Region &&
		a.AccessKeyID == b.AccessKeyID &&
		a.UseSSL == b.UseSSL &&
		a.ForcePathStyle == b.ForcePathStyle
}

// EndpointURL returns the normalized endpoint URL, applying UseSSL to custom endpoints.
func (c Configuration) EndpointURL() string {
	endpoint := strings.TrimSpace(c.Endpoint)
	if endpoint == "" {
		return ""
	}
	endpoint = strings.TrimPrefix(strings.TrimPrefix(endpoint, "https://"), "http://")
	scheme := "http://"
	if c.UseSSL {
		scheme = "https://"
	}
	return scheme + strings.TrimRight(endpoint, "/")
}

// RusticEnvironment builds the environment used by Rustic's OpenDAL S3 backend.
func (c Configuration) RusticEnvironment(rootParts ...string) []string {
	c = c.Normalized()
	repositoryParts := append([]string{"/", c.Prefix}, rootParts...)
	region := c.Region
	if region == "" {
		region = "auto"
	}
	environment := []string{
		"RUSTIC_REPOSITORY=opendal:s3",
		"RUSTIC_REPO_OPT_BUCKET=" + c.Bucket,
		"RUSTIC_REPO_OPT_ROOT=" + path.Join(repositoryParts...),
		"AWS_ACCESS_KEY_ID=" + c.AccessKeyID,
		"AWS_SECRET_ACCESS_KEY=" + c.SecretAccessKey,
		"AWS_EC2_METADATA_DISABLED=true",
	}
	if endpoint := c.EndpointURL(); endpoint != "" {
		environment = append(environment, "RUSTIC_REPO_OPT_ENDPOINT="+endpoint)
	}
	environment = append(environment,
		"RUSTIC_REPO_OPT_REGION="+region,
		"AWS_REGION="+region,
	)
	if !c.ForcePathStyle {
		environment = append(environment, "RUSTIC_REPO_OPT_ENABLE_VIRTUAL_HOST_STYLE=true")
	}
	return environment
}

// TestConnection verifies upload, download, and delete access for a destination.
func TestConnection(ctx context.Context, configuration Configuration) (err error) {
	configuration = configuration.Normalized()
	if err := configuration.Validate(true); err != nil {
		return err
	}
	client, err := newClientInternal(ctx, configuration)
	if err != nil {
		return fmt.Errorf("failed to upload S3 connection test object: %w", err)
	}
	payload := []byte(connectionTestPayload)
	remoteKey := path.Join(configuration.Prefix, ".arcane-connection-test-"+uuid.NewString())
	if err := putObjectInternal(ctx, client, configuration.Bucket, remoteKey, payload); err != nil {
		return fmt.Errorf("failed to upload S3 connection test object: %w", err)
	}

	deleted := false
	defer func() {
		if deleted {
			return
		}
		if cleanupErr := deleteObjectInternal(context.WithoutCancel(ctx), client, configuration.Bucket, remoteKey); cleanupErr != nil {
			err = errors.Join(err, fmt.Errorf("failed to clean up S3 connection test object: %w", cleanupErr))
		}
	}()

	downloaded, err := getObjectInternal(ctx, client, configuration.Bucket, remoteKey)
	if err != nil {
		return fmt.Errorf("failed to download S3 connection test object: %w", err)
	}
	if !bytes.Equal(downloaded, payload) {
		return errors.New("S3 connection test object contents did not match")
	}
	if err := deleteObjectInternal(ctx, client, configuration.Bucket, remoteKey); err != nil {
		return fmt.Errorf("failed to delete S3 connection test object: %w", err)
	}
	deleted = true
	return nil
}

func newClientInternal(ctx context.Context, configuration Configuration) (*awss3.Client, error) {
	region := configuration.Region
	if region == "" {
		region = "us-east-1"
	}
	awsConfiguration, err := awsconfig.LoadDefaultConfig(ctx,
		awsconfig.WithRegion(region),
		awsconfig.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(configuration.AccessKeyID, configuration.SecretAccessKey, "")),
	)
	if err != nil {
		return nil, err
	}
	endpoint := configuration.EndpointURL()
	return awss3.NewFromConfig(awsConfiguration, func(options *awss3.Options) {
		options.UsePathStyle = configuration.ForcePathStyle
		if endpoint != "" {
			options.BaseEndpoint = aws.String(endpoint)
		}
	}), nil
}

func putObjectInternal(ctx context.Context, client *awss3.Client, bucket, key string, payload []byte) error {
	_, err := client.PutObject(ctx, &awss3.PutObjectInput{
		Bucket:        aws.String(bucket),
		Key:           aws.String(key),
		Body:          bytes.NewReader(payload),
		ContentLength: new(int64(len(payload))),
		ContentType:   aws.String("application/gzip"),
	})
	return err
}

func getObjectInternal(ctx context.Context, client *awss3.Client, bucket, key string) ([]byte, error) {
	output, err := client.GetObject(ctx, &awss3.GetObjectInput{Bucket: aws.String(bucket), Key: aws.String(key)})
	if err != nil {
		return nil, err
	}
	payload, readErr := io.ReadAll(output.Body)
	closeErr := output.Body.Close()
	if readErr != nil {
		return nil, errors.Join(readErr, closeErr)
	}
	if closeErr != nil {
		return nil, closeErr
	}
	return payload, nil
}

func deleteObjectInternal(ctx context.Context, client *awss3.Client, bucket, key string) error {
	_, err := client.DeleteObject(ctx, &awss3.DeleteObjectInput{Bucket: aws.String(bucket), Key: aws.String(key)})
	return err
}
