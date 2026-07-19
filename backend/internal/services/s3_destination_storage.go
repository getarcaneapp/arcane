package services

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"path"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	backuptypes "github.com/getarcaneapp/arcane/types/v2/backup"
	"github.com/google/uuid"
)

func (s *S3DestinationService) clientInternal(ctx context.Context, cfg s3DestinationConfiguration) (*s3.Client, error) {
	region := strings.TrimSpace(cfg.S3Region)
	if region == "" {
		region = "us-east-1"
	}
	awsCfg, err := awsconfig.LoadDefaultConfig(ctx,
		awsconfig.WithRegion(region),
		awsconfig.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(cfg.S3AccessKeyID, cfg.S3SecretAccessKey, "")),
	)
	if err != nil {
		return nil, err
	}
	endpoint := normalizeS3EndpointInternal(cfg.S3Endpoint, cfg.S3UseSSL)
	return s3.NewFromConfig(awsCfg, func(options *s3.Options) {
		options.UsePathStyle = cfg.S3ForcePathStyle
		if endpoint != "" {
			options.BaseEndpoint = aws.String(endpoint)
		}
	}), nil
}

func normalizeS3EndpointInternal(endpoint string, useSSL bool) string {
	endpoint = strings.TrimSpace(endpoint)
	if endpoint == "" {
		return ""
	}
	endpoint = strings.TrimPrefix(strings.TrimPrefix(endpoint, "https://"), "http://")
	scheme := "http://"
	if useSSL {
		scheme = "https://"
	}
	return scheme + strings.TrimRight(endpoint, "/")
}

func (s *S3DestinationService) putObjectInternal(ctx context.Context, cfg s3DestinationConfiguration, body io.Reader, remoteKey string, size int64) error {
	client, err := s.clientInternal(ctx, cfg)
	if err != nil {
		return err
	}
	_, err = client.PutObject(ctx, &s3.PutObjectInput{
		Bucket:        aws.String(cfg.S3Bucket),
		Key:           aws.String(remoteKey),
		Body:          body,
		ContentLength: new(size),
		ContentType:   aws.String("application/gzip"),
	})
	return err
}

func (s *S3DestinationService) getObjectInternal(ctx context.Context, cfg s3DestinationConfiguration, remoteKey string) (io.ReadCloser, error) {
	client, err := s.clientInternal(ctx, cfg)
	if err != nil {
		return nil, err
	}
	output, err := client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(cfg.S3Bucket),
		Key:    aws.String(remoteKey),
	})
	if err != nil {
		return nil, err
	}
	return output.Body, nil
}

func (s *S3DestinationService) deleteObjectInternal(ctx context.Context, cfg s3DestinationConfiguration, remoteKey string) error {
	client, err := s.clientInternal(ctx, cfg)
	if err != nil {
		return err
	}
	_, err = client.DeleteObject(ctx, &s3.DeleteObjectInput{
		Bucket: aws.String(cfg.S3Bucket),
		Key:    aws.String(remoteKey),
	})
	return err
}

func (s *S3DestinationService) TestS3Destination(ctx context.Context, id string, input *backuptypes.UpdateS3Destination) (err error) {
	cfg, err := s.configurationInternal(ctx, id)
	if err != nil {
		return err
	}
	if input != nil {
		if err := validateS3DestinationInputInternal(input.Name, input.Endpoint, input.Bucket, input.Region, input.AccessKeyID, input.SecretAccessKey, false); err != nil {
			return err
		}
		cfg.S3Endpoint = strings.TrimSpace(input.Endpoint)
		cfg.S3Bucket = strings.TrimSpace(input.Bucket)
		cfg.S3Region = strings.TrimSpace(input.Region)
		cfg.S3AccessKeyID = strings.TrimSpace(input.AccessKeyID)
		cfg.S3Prefix = strings.Trim(strings.TrimSpace(input.Prefix), "/")
		cfg.S3UseSSL = input.UseSSL
		cfg.S3ForcePathStyle = input.ForcePathStyle
		if strings.TrimSpace(input.SecretAccessKey) != "" {
			cfg.S3SecretAccessKey = strings.TrimSpace(input.SecretAccessKey)
		}
	}

	payload := []byte("arcane-s3-connection-test")
	remoteKey := path.Join(cfg.S3Prefix, ".arcane-connection-test-"+uuid.NewString())
	if err := s.putObjectInternal(ctx, cfg, bytes.NewReader(payload), remoteKey, int64(len(payload))); err != nil {
		return fmt.Errorf("failed to upload S3 connection test object: %w", err)
	}

	deleted := false
	defer func() {
		if !deleted {
			if cleanupErr := s.deleteObjectInternal(context.WithoutCancel(ctx), cfg, remoteKey); cleanupErr != nil {
				err = errors.Join(err, fmt.Errorf("failed to clean up S3 connection test object: %w", cleanupErr))
			}
		}
	}()

	object, err := s.getObjectInternal(ctx, cfg, remoteKey)
	if err != nil {
		return fmt.Errorf("failed to download S3 connection test object: %w", err)
	}
	downloaded, readErr := io.ReadAll(object)
	closeErr := object.Close()
	if readErr != nil {
		return errors.Join(fmt.Errorf("failed to read S3 connection test object: %w", readErr), closeErr)
	}
	if closeErr != nil {
		return fmt.Errorf("failed to close S3 connection test object: %w", closeErr)
	}
	if !bytes.Equal(downloaded, payload) {
		return errors.New("S3 connection test object contents did not match")
	}

	if err := s.deleteObjectInternal(ctx, cfg, remoteKey); err != nil {
		return fmt.Errorf("failed to delete S3 connection test object: %w", err)
	}
	deleted = true
	return nil
}
