package mainconfig

import (
	"context"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/sqs"
	appconfig "github.com/wolfman30/medspa-ai-platform/internal/config"
)

// LoadAWSConfig centralizes AWS SDK initialization so both binaries share the
// same LocalStack/production wiring.
func LoadAWSConfig(ctx context.Context, cfg *appconfig.Config) (aws.Config, error) {
	loaders := []func(*config.LoadOptions) error{
		config.WithRegion(cfg.AWSRegion),
		config.WithCredentialsProvider(
			credentials.NewStaticCredentialsProvider(cfg.AWSAccessKeyID, cfg.AWSSecretAccessKey, ""),
		),
	}

	awsCfg, err := config.LoadDefaultConfig(ctx, loaders...)
	if err != nil {
		return aws.Config{}, err
	}

	if endpoint := cfg.AWSEndpointOverride; endpoint != "" {
		awsCfg.EndpointResolverWithOptions = aws.EndpointResolverWithOptionsFunc(
			func(service, region string, _ ...interface{}) (aws.Endpoint, error) {
				if service == sqs.ServiceID {
					return aws.Endpoint{
						URL:           endpoint,
						PartitionID:   "aws",
						SigningRegion: cfg.AWSRegion,
					}, nil
				}
				return aws.Endpoint{}, &aws.EndpointNotFoundError{}
			},
		)
	}

	return awsCfg, nil
}
