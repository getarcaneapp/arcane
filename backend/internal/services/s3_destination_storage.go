package services

import (
	"context"
	"io"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

func (s *S3DestinationService) clientInternal(ctx context.Context, cfg s3DestinationConfiguration) (*s3.Client, error) {
	awsCfg, err := awsconfig.LoadDefaultConfig(ctx,
		awsconfig.WithRegion(cfg.S3Region),
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
