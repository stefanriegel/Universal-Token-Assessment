package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

const module = "github.com/stefanriegel/Universal-Token-Assessment/"

var allowedTests = map[string]map[string]bool{
	"internal/exporter": {"TestCrossSourceAgreement": true, "TestNiosServerMetricFieldDrift": true},
	"internal/scanner/nios": {
		"TestMSAllocationUnavailableGolden": true, "TestMSAllocation_Parity": true,
		"TestMSAllocation_Parity/both": true, "TestMSAllocation_Parity/dns-only": true,
		"TestMSAllocation_Parity/dhcp-only": true, "TestMSAllocation_Parity/held-back": true,
		"TestMSAllocation_Parity/absent": true, "TestMSAllocation_Parity/unavailable": true,
		"TestMSAllocation_Parity/boundary-exact": true, "TestMSAllocation_Parity/boundary-plus-one": true,
		"TestMSAllocation_Parity_Adjacency": true, "TestMSAllocation_Parity_Boundary": true,
		"TestMSAllocation_Parity_Distinguishable": true,
	},
	"server": {"TestServerNiosServerMetricFieldDrift": true},
}

var requiredTests = func() map[string]bool {
	m := map[string]bool{}
	for pkg, tests := range allowedTests {
		for test := range tests {
			if test != "TestMSAllocation_Parity" {
				m[pkg+"\x00"+test] = true
			}
		}
	}
	return m
}()

var expectedPaths = map[string][]string{
	"internal/exporter\x00TestCrossSourceAgreement":                      {"testdata/cross-source-fixture.json"},
	"internal/exporter\x00TestNiosServerMetricFieldDrift":                {"testdata/nios-metric-fields.json"},
	"internal/scanner/nios\x00TestMSAllocationUnavailableGolden":         {"testdata/ms-allocation/unavailable.xml"},
	"internal/scanner/nios\x00TestMSAllocation_Parity/both":              {"testdata/ms-allocation/both.xml"},
	"internal/scanner/nios\x00TestMSAllocation_Parity/dns-only":          {"testdata/ms-allocation/dns-only.xml"},
	"internal/scanner/nios\x00TestMSAllocation_Parity/dhcp-only":         {"testdata/ms-allocation/dhcp-only.xml"},
	"internal/scanner/nios\x00TestMSAllocation_Parity/held-back":         {"testdata/ms-allocation/held-back.xml"},
	"internal/scanner/nios\x00TestMSAllocation_Parity/absent":            {"testdata/ms-allocation/absent.xml"},
	"internal/scanner/nios\x00TestMSAllocation_Parity/unavailable":       {"testdata/ms-allocation/unavailable.json"},
	"internal/scanner/nios\x00TestMSAllocation_Parity/boundary-exact":    {"testdata/ms-allocation/boundary-exact.xml"},
	"internal/scanner/nios\x00TestMSAllocation_Parity/boundary-plus-one": {"testdata/ms-allocation/boundary-plus-one.xml"},
	"internal/scanner/nios\x00TestMSAllocation_Parity_Adjacency":         {"testdata/ms-allocation/both.json"},
	"internal/scanner/nios\x00TestMSAllocation_Parity_Boundary":          {"testdata/ms-allocation/boundary-exact.json"},
	"internal/scanner/nios\x00TestMSAllocation_Parity_Distinguishable":   {"testdata/ms-allocation/absent.json"},
	"server\x00TestServerNiosServerMetricFieldDrift":                     {"testdata/nios-metric-fields.json"},
}

var fixturePath = regexp.MustCompile(`testdata/[A-Za-z0-9_./-]+\.(?:json|xml)`)
var harnessOutput = regexp.MustCompile(`^(?:=== (?:RUN|PAUSE|CONT) +|--- FAIL: )`)

type event struct{ Action, Package, Test, Output string }

func verify(path string, exitCode int) error {
	return verifyTo(path, exitCode, os.Stdout)
}

func verifyTo(path string, exitCode int, out io.Writer) error {
	if exitCode == 0 {
		return errors.New("repository-wide tests unexpectedly passed while known-fixture manifest is active")
	}
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	failedTests, failedPackages, outputs, packageOutputs := map[string]bool{}, map[string]bool{}, map[string]string{}, map[string]string{}
	s := bufio.NewScanner(f)
	s.Buffer(make([]byte, 64*1024), 4*1024*1024)
	count := 0
	for s.Scan() {
		count++
		var e event
		if err := json.Unmarshal(s.Bytes(), &e); err != nil {
			return fmt.Errorf("malformed JSON at line %d: %w", count, err)
		}
		pkg := strings.TrimPrefix(e.Package, module)
		if e.Test != "" {
			outputs[pkg+"\x00"+e.Test] += e.Output
		} else if e.Output != "" {
			packageOutputs[pkg] += e.Output
		}
		if e.Action != "fail" {
			continue
		}
		if _, ok := allowedTests[pkg]; !ok {
			return fmt.Errorf("unexpected failing package %q", pkg)
		}
		if e.Test == "" {
			failedPackages[pkg] = true
			continue
		}
		if !allowedTests[pkg][e.Test] {
			return fmt.Errorf("unexpected failing test %s/%s", pkg, e.Test)
		}
		failedTests[pkg+"\x00"+e.Test] = true
	}
	if err := s.Err(); err != nil {
		return err
	}
	if count == 0 {
		return errors.New("empty JSON stream")
	}
	for key := range requiredTests {
		if !failedTests[key] {
			return fmt.Errorf("missing expected failing test %s", strings.ReplaceAll(key, "\x00", "/"))
		}
	}
	for pkg := range allowedTests {
		if !failedPackages[pkg] {
			return fmt.Errorf("missing expected failing package %s", pkg)
		}
	}
	for key := range failedTests {
		out := outputs[key]
		matched := strings.HasSuffix(key, "\x00TestMSAllocation_Parity")
		observedPaths := map[string]bool{}
		for _, line := range strings.Split(strings.TrimSuffix(out, "\n"), "\n") {
			if line == "" {
				continue
			}
			if harnessOutput.MatchString(line) {
				continue
			}
			paths := fixturePath.FindAllString(line, -1)
			if !strings.Contains(line, "no such file or directory") || len(paths) == 0 {
				return fmt.Errorf("non-fixture failure diagnostic in %s", strings.ReplaceAll(key, "\x00", "/"))
			}
			for _, p := range paths {
				observedPaths[p], matched = true, true
			}
		}
		if !matched {
			return fmt.Errorf("missing fixture diagnostic in %s", strings.ReplaceAll(key, "\x00", "/"))
		}
		expected := expectedPaths[key]
		if len(observedPaths) != len(expected) {
			return fmt.Errorf("fixture paths in %s: got %v, want %v", strings.ReplaceAll(key, "\x00", "/"), sortedKeys(observedPaths), expected)
		}
		for _, p := range expected {
			if !observedPaths[p] {
				return fmt.Errorf("fixture paths in %s: got %v, want %v", strings.ReplaceAll(key, "\x00", "/"), sortedKeys(observedPaths), expected)
			}
		}
	}
	for pkg := range failedPackages {
		for _, line := range strings.Split(strings.TrimSpace(packageOutputs[pkg]), "\n") {
			if line == "" {
				continue
			}
			if line != "FAIL" && !strings.HasPrefix(line, "FAIL\t"+module+pkg+"\t") {
				return fmt.Errorf("non-fixture package diagnostic in %s", pkg)
			}
		}
	}
	tests := sortedKeys(failedTests)
	paths := map[string]bool{}
	for _, expected := range expectedPaths {
		for _, p := range expected {
			paths[p] = true
		}
	}
	for i := range tests {
		tests[i] = strings.ReplaceAll(tests[i], "\x00", "/")
	}
	fmt.Fprintf(out, "accepted known missing-fixture failures\ntests (%d): %s\npaths (%d): %s\n", len(tests), strings.Join(tests, ", "), len(paths), strings.Join(sortedKeys(paths), ", "))
	return nil
}

func sortedKeys(m map[string]bool) []string {
	keys := make([]string, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func main() {
	if len(os.Args) != 3 {
		fmt.Fprintln(os.Stderr, "usage: verify-known-fixtures <go-test.json> <exit-code>")
		os.Exit(2)
	}
	code, err := strconv.Atoi(os.Args[2])
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	if err := verify(os.Args[1], code); err != nil {
		fmt.Fprintln(os.Stderr, "known-fixture verification failed:", err)
		os.Exit(1)
	}
}
