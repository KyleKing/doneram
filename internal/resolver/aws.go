package resolver

import (
	"context"
	"fmt"
	"strings"

	"github.com/kyleking/doneram/internal/cloud"
	"github.com/kyleking/doneram/internal/parser"
)

// ErrUnavailable is cloud.ErrUnavailable: a lookup that needs a cloud CLI or
// credentials this machine does not have. The engine reports the site as
// skipped rather than failed.
var ErrUnavailable = cloud.ErrUnavailable

// splitRegion reads an optional "@<region>" suffix off a resolver name.
// Without one, the CLI's own default region (AWS_REGION, the profile)
// applies.
func splitRegion(name string) (string, []string) {
	if base, region, ok := strings.Cut(name, "@"); ok && region != "" {
		return base, []string{"--region", region}
	}
	return name, nil
}

// AWSSSMResolver reads an SSM parameter, typically one of AWS's public ones
// under /aws/service/ (the newest Amazon Linux or Bottlerocket AMI, an EKS
// optimized AMI), so an AMI ID tracks what AWS itself calls latest.
// resolverName is the parameter, optionally with "@<region>".
type AWSSSMResolver struct{ Run cloud.Runner }

func NewAWSSSMResolver() *AWSSSMResolver { return &AWSSSMResolver{Run: cloud.Run} }

func (*AWSSSMResolver) Name() string { return "aws-ssm" }

func (r *AWSSSMResolver) Resolve(ctx context.Context, name string, _ *parser.VersionPattern) (string, error) {
	param, region := splitRegion(name)
	args := append([]string{"ssm", "get-parameter", "--name", param, "--query", "Parameter.Value", "--output", "text"}, region...)
	value, err := r.Run(ctx, "aws", args...)
	if err != nil {
		return "", err
	}
	if value == "" || value == "None" {
		return "", fmt.Errorf("SSM parameter %s has no value", param)
	}
	return value, nil
}

func (*AWSSSMResolver) GetChangelog(context.Context, string, string, string) (string, error) {
	return "", nil
}

// AWSAMIResolver finds the newest available AMI whose name matches a
// pattern, for an AMI no SSM parameter tracks (a vendor's or your own
// build). resolverName is "<owner>/<name pattern>", optionally with
// "@<region>": "amazon/al2023-ami-2023.*-x86_64@us-east-1". The owner is an
// account ID or an alias (amazon, aws-marketplace, self).
type AWSAMIResolver struct{ Run cloud.Runner }

func NewAWSAMIResolver() *AWSAMIResolver { return &AWSAMIResolver{Run: cloud.Run} }

func (*AWSAMIResolver) Name() string { return "aws-ami" }

func (r *AWSAMIResolver) Resolve(ctx context.Context, name string, _ *parser.VersionPattern) (string, error) {
	spec, region := splitRegion(name)
	owner, pattern, ok := strings.Cut(spec, "/")
	if !ok || owner == "" || pattern == "" {
		return "", fmt.Errorf("aws-ami resolverName %q: want <owner>/<name pattern>[@region]", name)
	}
	args := append([]string{
		"ec2", "describe-images", "--owners", owner,
		"--filters", "Name=name,Values=" + pattern, "Name=state,Values=available",
		"--query", "sort_by(Images, &CreationDate)[-1].ImageId", "--output", "text",
	}, region...)
	id, err := r.Run(ctx, "aws", args...)
	if err != nil {
		return "", err
	}
	if !strings.HasPrefix(id, "ami-") {
		return "", fmt.Errorf("no available AMI owned by %s matches %s", owner, pattern)
	}
	return id, nil
}

func (*AWSAMIResolver) GetChangelog(context.Context, string, string, string) (string, error) {
	return "", nil
}
