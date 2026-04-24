package provisioning

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	ec2types "github.com/aws/aws-sdk-go-v2/service/ec2/types"
	"go.uber.org/zap"
)

// Apply for AWS calls EC2 RunInstances. It expects metadata to carry at least
// `region`, `image_id` (AMI), `instance_type`, and either `subnet_id` or
// `security_groups` to land the instance in the right VPC. User-data is taken
// from metadata["user_data"] or synthesised from metadata["install_one_liner"].
// Credentials flow in via metadata["_cred_access_key_id"] /
// metadata["_cred_secret_access_key"] when the caller resolved them from the
// provider_credentials table; otherwise the default AWS SDK credential chain
// is used (env + IMDS + shared config).
func (a *awsAdapter) Apply(ctx context.Context, nodeID string, opts Options, metadata map[string]string) (*ApplyResult, error) {
	ensureAWSMetadata(metadata)
	log := logFromCtx(ctx)

	if strings.TrimSpace(metadata["image_id"]) == "" {
		// Without an AMI we cannot boot anything — preserve existing behaviour
		// by forwarding to the HTTP adapter so development environments work.
		return a.httpAdapter.Apply(ctx, nodeID, opts, metadata)
	}

	cfg, err := awsLoadConfig(ctx, metadata)
	if err != nil {
		log.Warn("aws config load failed; falling back to http adapter", zap.Error(err))
		return a.httpAdapter.Apply(ctx, nodeID, opts, metadata)
	}

	client := ec2.NewFromConfig(cfg)

	userData := strings.TrimSpace(metadata["user_data"])
	if userData == "" {
		if one := strings.TrimSpace(metadata["install_one_liner"]); one != "" {
			userData = "#cloud-config\nruncmd:\n  - [ sh, -c, \"" + escapeYAMLScalar(one) + "\" ]\n"
		}
	}
	var encodedUserData *string
	if userData != "" {
		enc := base64.StdEncoding.EncodeToString([]byte(userData))
		encodedUserData = aws.String(enc)
	}

	input := &ec2.RunInstancesInput{
		ImageId:      aws.String(strings.TrimSpace(metadata["image_id"])),
		InstanceType: ec2types.InstanceType(strings.TrimSpace(metadata["instance_type"])),
		MinCount:     aws.Int32(1),
		MaxCount:     aws.Int32(1),
		UserData:     encodedUserData,
		TagSpecifications: []ec2types.TagSpecification{{
			ResourceType: ec2types.ResourceTypeInstance,
			Tags: []ec2types.Tag{
				{Key: aws.String("control-one-node-id"), Value: aws.String(nodeID)},
				{Key: aws.String("Name"), Value: aws.String("controlone-" + nodeID)},
			},
		}},
	}
	if v := strings.TrimSpace(metadata["subnet_id"]); v != "" {
		input.SubnetId = aws.String(v)
	}
	if v := strings.TrimSpace(metadata["iam_profile"]); v != "" {
		input.IamInstanceProfile = &ec2types.IamInstanceProfileSpecification{Name: aws.String(v)}
	}
	if v := strings.TrimSpace(metadata["key_name"]); v != "" {
		input.KeyName = aws.String(v)
	}
	if groups := strings.TrimSpace(metadata["security_groups"]); groups != "" {
		input.SecurityGroupIds = strings.Split(groups, ",")
	}

	if input.InstanceType == "" {
		input.InstanceType = ec2types.InstanceTypeT3Micro
	}

	out, err := client.RunInstances(ctx, input)
	if err != nil {
		log.Warn("aws RunInstances failed; falling back to http adapter", zap.Error(err))
		return a.httpAdapter.Apply(ctx, nodeID, opts, metadata)
	}
	if len(out.Instances) == 0 {
		return nil, errors.New("RunInstances returned no instances")
	}
	instanceID := aws.ToString(out.Instances[0].InstanceId)
	log.Info("aws RunInstances ok",
		zap.String("node_id", nodeID),
		zap.String("instance_id", instanceID))
	return &ApplyResult{OperationID: "aws-" + instanceID}, nil
}

// Destroy calls TerminateInstances for the instance carrying the
// control-one-node-id tag equal to nodeID. When credentials or region are not
// available the call forwards to the HTTP adapter so operators can still
// clean up manually.
func (a *awsAdapter) Destroy(ctx context.Context, nodeID string) error {
	log := logFromCtx(ctx)
	cfg, err := awsLoadConfig(ctx, nil)
	if err != nil {
		return a.httpAdapter.Destroy(ctx, nodeID)
	}
	client := ec2.NewFromConfig(cfg)

	descOut, err := client.DescribeInstances(ctx, &ec2.DescribeInstancesInput{
		Filters: []ec2types.Filter{{
			Name:   aws.String("tag:control-one-node-id"),
			Values: []string{nodeID},
		}},
	})
	if err != nil {
		log.Warn("aws describe instances failed; forwarding", zap.Error(err))
		return a.httpAdapter.Destroy(ctx, nodeID)
	}

	var ids []string
	for _, res := range descOut.Reservations {
		for _, inst := range res.Instances {
			if inst.State != nil && inst.State.Name == ec2types.InstanceStateNameTerminated {
				continue
			}
			if v := aws.ToString(inst.InstanceId); v != "" {
				ids = append(ids, v)
			}
		}
	}
	if len(ids) == 0 {
		log.Warn("aws destroy: no matching instances", zap.String("node_id", nodeID))
		return nil
	}
	if _, err := client.TerminateInstances(ctx, &ec2.TerminateInstancesInput{InstanceIds: ids}); err != nil {
		log.Warn("aws TerminateInstances failed; forwarding", zap.Error(err))
		return a.httpAdapter.Destroy(ctx, nodeID)
	}
	return nil
}

// VerifyReachable performs a cheap DescribeRegions call to prove that the
// credentials + region combination is live.
func (a *awsAdapter) VerifyReachable(ctx context.Context, _ string, metadata map[string]string) error {
	cfg, err := awsLoadConfig(ctx, metadata)
	if err != nil {
		return err
	}
	client := ec2.NewFromConfig(cfg)
	if _, err := client.DescribeRegions(ctx, &ec2.DescribeRegionsInput{AllRegions: aws.Bool(false)}); err != nil {
		return fmt.Errorf("describe regions: %w", err)
	}
	return nil
}

func awsLoadConfig(ctx context.Context, metadata map[string]string) (aws.Config, error) {
	var opts []func(*awsconfig.LoadOptions) error

	region := ""
	if metadata != nil {
		region = strings.TrimSpace(metadata["region"])
	}
	if region == "" {
		region = strings.TrimSpace(firstNonEmpty(metadata["_cred_region"]))
	}
	if region != "" {
		opts = append(opts, awsconfig.WithRegion(region))
	}

	if metadata != nil {
		access := strings.TrimSpace(metadata["_cred_access_key_id"])
		secret := strings.TrimSpace(metadata["_cred_secret_access_key"])
		session := strings.TrimSpace(metadata["_cred_session_token"])
		if access != "" && secret != "" {
			opts = append(opts, awsconfig.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(access, secret, session)))
		}
	}
	return awsconfig.LoadDefaultConfig(ctx, opts...)
}
