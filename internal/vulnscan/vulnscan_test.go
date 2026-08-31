package vulnscan

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

// fakeBinary writes an executable shell script to a temp dir that prints
// stdout on invocation, mimicking trivy/grype's JSON-on-stdout contract
// without requiring either tool to be installed.
func fakeBinary(t *testing.T, name, stdout string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, name)
	script := "#!/bin/sh\ncat <<'EOF'\n" + stdout + "\nEOF\n"
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatalf("writing fake binary: %v", err)
	}
	return path
}

func TestTrivyScanner_ScanImage(t *testing.T) {
	report := `{
  "Results": [
    {
      "Vulnerabilities": [
        {
          "VulnerabilityID": "CVE-2023-42363",
          "PkgName": "busybox",
          "InstalledVersion": "1.36.1-r15",
          "FixedVersion": "1.36.1-r17",
          "Severity": "HIGH",
          "PrimaryURL": "https://avd.aquasec.com/nvd/cve-2023-42363"
        }
      ]
    }
  ]
}`
	bin := fakeBinary(t, "trivy", report)
	s := NewTrivyScanner(bin)

	findings, err := s.ScanImage(context.Background(), "alpine:3.19")
	if err != nil {
		t.Fatalf("ScanImage() error = %v", err)
	}
	if len(findings) != 1 {
		t.Fatalf("ScanImage() = %d findings, want 1", len(findings))
	}
	got := findings[0]
	if got.Package != "busybox" || got.Fixed != "1.36.1-r17" || got.Severity != "HIGH" {
		t.Errorf("ScanImage() = %+v, unexpected fields", got)
	}
}

func TestGrypeScanner_ScanImage(t *testing.T) {
	report := `{
  "matches": [
    {
      "vulnerability": {
        "id": "CVE-2023-42363",
        "severity": "High",
        "dataSource": "https://nvd.nist.gov/vuln/detail/CVE-2023-42363",
        "fix": {"versions": ["1.36.1-r17"]}
      },
      "artifact": {"name": "busybox", "version": "1.36.1-r15"}
    }
  ]
}`
	bin := fakeBinary(t, "grype", report)
	s := NewGrypeScanner(bin)

	findings, err := s.ScanImage(context.Background(), "alpine:3.19")
	if err != nil {
		t.Fatalf("ScanImage() error = %v", err)
	}
	if len(findings) != 1 {
		t.Fatalf("ScanImage() = %d findings, want 1", len(findings))
	}
	got := findings[0]
	if got.Package != "busybox" || got.Fixed != "1.36.1-r17" || got.Severity != "High" {
		t.Errorf("ScanImage() = %+v, unexpected fields", got)
	}
}

func TestDetect(t *testing.T) {
	scanner, ok := Detect()
	if !ok {
		t.Skip("neither trivy nor grype installed on this machine")
	}
	if scanner.Name() != "trivy" && scanner.Name() != "grype" {
		t.Errorf("Detect() returned unexpected scanner %q", scanner.Name())
	}
}
