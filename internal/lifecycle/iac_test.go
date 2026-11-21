package lifecycle

import (
	"strings"
	"testing"
)

func TestScanIaC(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "infra/main.tf", `resource "aws_lambda_function" "f" {
  runtime = "python3.9"
}

resource "aws_db_instance" "db" {
  engine         = "postgres"
  engine_version = "13.7"
}

resource "aws_rds_cluster" "c" {
  engine         = "aurora-postgresql"
  engine_version = "14.6"
}

resource "aws_eks_cluster" "k" {
  version = "1.27"
}

resource "aws_elasticache_replication_group" "r" {
  engine_version = "6.2"
}

resource "aws_opensearch_domain" "s" {
  engine_version = "OpenSearch_2.11"
}

resource "aws_s3_bucket" "b" {
  version = "ignored"
}

resource "aws_db_instance" "var" {
  engine_version = var.pg
}
`)
	write(t, dir, "serverless.yml", "provider:\n  runtime: nodejs16.x\n")
	write(t, dir, "template.yaml", "Resources:\n  F:\n    Properties:\n      Runtime: java11\n")
	write(t, dir, "src/client.ts", "headers: { 'X-GitHub-Api-Version': '2022-11-28' }\n")

	refs, err := Scan(dir)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, r := range refs {
		got = append(got, string(r.Source)+":"+r.Product+"@"+r.Version)
	}
	want := []string{
		"service:aws-lambda@python3.9",
		"service:amazon-rds-postgresql@13.7",
		"service:amazon-aurora-postgresql@14.6",
		"service:amazon-eks@1.27",
		"service:amazon-elasticache-redis@6.2",
		"service:amazon-opensearch@2.11",
		"service:aws-lambda@nodejs16.x",
		"api:github-rest-api@2022-11-28",
		"service:aws-lambda@java11",
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("Scan() =\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}
