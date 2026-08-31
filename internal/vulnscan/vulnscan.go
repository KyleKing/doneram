// Package vulnscan shells out to trivy or grype to read a Docker image's
// layers statically, since OSV has no way to map an image tag to the
// packages installed inside it.
package vulnscan

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
)

// Finding is one vulnerability a scanner found in an image, trimmed to what
// a report needs.
type Finding struct {
	Package   string
	Installed string
	Fixed     string
	ID        string
	Severity  string
	URL       string
}

// Scanner scans a Docker image tag for known vulnerabilities in its layers.
type Scanner interface {
	Name() string
	ScanImage(ctx context.Context, image string) ([]Finding, error)
}

// Detect returns the configured scanner, preferring trivy over grype when
// both are on PATH, or false when neither is available.
func Detect() (Scanner, bool) {
	if path, err := exec.LookPath("trivy"); err == nil {
		return &TrivyScanner{bin: path}, true
	}
	if path, err := exec.LookPath("grype"); err == nil {
		return &GrypeScanner{bin: path}, true
	}
	return nil, false
}

// TrivyScanner runs `trivy image` and parses its JSON report.
type TrivyScanner struct {
	bin string
}

func NewTrivyScanner(bin string) *TrivyScanner {
	if bin == "" {
		bin = "trivy"
	}
	return &TrivyScanner{bin: bin}
}

func (s *TrivyScanner) Name() string { return "trivy" }

type trivyReport struct {
	Results []struct {
		Vulnerabilities []struct {
			VulnerabilityID  string `json:"VulnerabilityID"`
			PkgName          string `json:"PkgName"`
			InstalledVersion string `json:"InstalledVersion"`
			FixedVersion     string `json:"FixedVersion"`
			Severity         string `json:"Severity"`
			PrimaryURL       string `json:"PrimaryURL"`
		} `json:"Vulnerabilities"`
	} `json:"Results"`
}

func (s *TrivyScanner) ScanImage(ctx context.Context, image string) ([]Finding, error) {
	cmd := exec.CommandContext(ctx, s.bin, "image", "--format", "json", "--quiet", image)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("trivy image %s: %w: %s", image, err, strings.TrimSpace(string(out)))
	}

	var report trivyReport
	if err := json.Unmarshal(out, &report); err != nil {
		return nil, fmt.Errorf("parsing trivy output for %s: %w", image, err)
	}

	var findings []Finding
	for _, result := range report.Results {
		for _, v := range result.Vulnerabilities {
			findings = append(findings, Finding{
				Package:   v.PkgName,
				Installed: v.InstalledVersion,
				Fixed:     v.FixedVersion,
				ID:        v.VulnerabilityID,
				Severity:  v.Severity,
				URL:       v.PrimaryURL,
			})
		}
	}
	return findings, nil
}

// GrypeScanner runs `grype <image> -o json` and parses its report.
type GrypeScanner struct {
	bin string
}

func NewGrypeScanner(bin string) *GrypeScanner {
	if bin == "" {
		bin = "grype"
	}
	return &GrypeScanner{bin: bin}
}

func (s *GrypeScanner) Name() string { return "grype" }

type grypeReport struct {
	Matches []struct {
		Vulnerability struct {
			ID         string `json:"id"`
			Severity   string `json:"severity"`
			DataSource string `json:"dataSource"`
			Fix        struct {
				Versions []string `json:"versions"`
			} `json:"fix"`
		} `json:"vulnerability"`
		Artifact struct {
			Name    string `json:"name"`
			Version string `json:"version"`
		} `json:"artifact"`
	} `json:"matches"`
}

func (s *GrypeScanner) ScanImage(ctx context.Context, image string) ([]Finding, error) {
	cmd := exec.CommandContext(ctx, s.bin, image, "-o", "json")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("grype %s: %w: %s", image, err, strings.TrimSpace(string(out)))
	}

	var report grypeReport
	if err := json.Unmarshal(out, &report); err != nil {
		return nil, fmt.Errorf("parsing grype output for %s: %w", image, err)
	}

	var findings []Finding
	for _, m := range report.Matches {
		var fixed string
		if len(m.Vulnerability.Fix.Versions) > 0 {
			fixed = m.Vulnerability.Fix.Versions[0]
		}
		findings = append(findings, Finding{
			Package:   m.Artifact.Name,
			Installed: m.Artifact.Version,
			Fixed:     fixed,
			ID:        m.Vulnerability.ID,
			Severity:  m.Vulnerability.Severity,
			URL:       m.Vulnerability.DataSource,
		})
	}
	return findings, nil
}
