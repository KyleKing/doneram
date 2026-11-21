package lifecycle

import (
	"regexp"
	"strings"
)

// Managed AWS services retire versions on their own schedule, and a
// resource left on one is upgraded for you during a maintenance window or
// starts costing extended-support fees. These are read from Terraform,
// CloudFormation/SAM, and Serverless Framework files.
const (
	// Service is a managed runtime or engine version in infrastructure code.
	Service Source = "service"
	// API is a pinned API version header in code.
	API Source = "api"
)

// lambdaRuntime is a Lambda runtime identifier ("python3.9", "nodejs18.x",
// "java17", "provided.al2"), written as an attribute in any of the formats.
var lambdaRuntime = regexp.MustCompile(`(?i)\bruntime"?\s*[:=]\s*["']?((?:python|nodejs|java|dotnet|ruby|provided|go)[\w.]*)["']?`)

// tfBlock opens a Terraform resource whose versions live in attributes.
var tfBlock = regexp.MustCompile(`^\s*resource\s+"(aws_[a-z_]+)"`)

var tfAttr = regexp.MustCompile(`^\s*(engine|engine_version|version|cluster_version|kubernetes_version)\s*=\s*"([^"]+)"`)

// rdsProducts maps an RDS or Aurora engine to its endoflife.date product.
var rdsProducts = map[string]string{
	"postgres":          "amazon-rds-postgresql",
	"mysql":             "amazon-rds-mysql",
	"mariadb":           "amazon-rds-mariadb",
	"aurora-postgresql": "amazon-aurora-postgresql",
	"aurora-mysql":      "amazon-aurora-mysql",
}

func isIaC(rel, lower string) bool {
	return strings.HasSuffix(lower, ".tf") || lower == "serverless.yml" || lower == "serverless.yaml" ||
		lower == "template.yaml" || lower == "template.yml" || lower == "template.json" ||
		strings.Contains(rel, "cloudformation/") || strings.HasSuffix(lower, ".template.json") ||
		strings.HasSuffix(lower, ".cfn.yaml") || strings.HasSuffix(lower, ".cfn.yml")
}

func scanIaC(path string) ([]Ref, error) {
	ls, err := lines(path)
	if err != nil {
		return nil, err
	}
	var out []Ref
	resource := ""
	attrs := map[string]string{}
	attrLine := map[string]int{}
	flush := func() {
		if resource == "" {
			return
		}
		ver, line := attrs["engine_version"], attrLine["engine_version"]
		var product string
		switch resource {
		case "aws_db_instance", "aws_rds_cluster":
			product = rdsProducts[attrs["engine"]]
			if product == "" && resource == "aws_rds_cluster" && attrs["engine"] == "" {
				product = "amazon-aurora-mysql"
			}
		case "aws_elasticache_cluster", "aws_elasticache_replication_group":
			product = "amazon-elasticache-redis"
		case "aws_opensearch_domain", "aws_elasticsearch_domain":
			product = "amazon-opensearch"
			ver = strings.TrimPrefix(ver, "OpenSearch_")
			if strings.HasPrefix(ver, "Elasticsearch_") {
				product = ""
			}
		case "aws_eks_cluster":
			product, ver, line = "amazon-eks", attrs["version"], attrLine["version"]
		}
		if product != "" && ver != "" && !strings.Contains(ver, "$") {
			out = append(out, Ref{Source: Service, File: path, Line: line, Name: resource + " " + ver, Product: product, Version: ver})
		}
		resource = ""
		attrs, attrLine = map[string]string{}, map[string]int{}
	}
	for i, line := range ls {
		if m := lambdaRuntime.FindStringSubmatch(line); m != nil {
			out = append(out, Ref{Source: Service, File: path, Line: i + 1, Name: "lambda " + m[1], Product: "aws-lambda", Version: strings.ToLower(m[1]), ByID: true})
		}
		if m := tfBlock.FindStringSubmatch(line); m != nil {
			flush()
			resource = m[1]
			continue
		}
		if resource == "" {
			continue
		}
		if strings.HasPrefix(line, "}") {
			flush()
			continue
		}
		if m := tfAttr.FindStringSubmatch(line); m != nil {
			attrs[m[1]], attrLine[m[1]] = m[2], i+1
		}
	}
	flush()
	return out, nil
}

// githubAPIVersion is a pinned GitHub REST API version, sent as the
// X-GitHub-Api-Version header. GitHub supports each for at least two years
// after its successor ships and then rejects requests naming it.
var githubAPIVersion = regexp.MustCompile(`(?i)X-GitHub-Api-Version["']?\s*[:=,]\s*["']?(\d{4}-\d{2}-\d{2})`)

func scanAPIVersions(path string) ([]Ref, error) {
	ls, err := lines(path)
	if err != nil {
		return nil, err
	}
	var out []Ref
	for i, line := range ls {
		if m := githubAPIVersion.FindStringSubmatch(line); m != nil {
			out = append(out, Ref{Source: API, File: path, Line: i + 1, Name: "X-GitHub-Api-Version " + m[1], Product: GitHubAPI, Version: m[1], ByID: true})
		}
	}
	return out, nil
}

// GitHubAPI is not an endoflife.date product: its supported versions come
// from GitHub's own /versions endpoint, and the caller looks it up there.
const GitHubAPI = "github-rest-api"
