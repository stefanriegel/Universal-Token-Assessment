package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func fixtureStream(t *testing.T) string {
	t.Helper()
	var b strings.Builder
	emit := func(e event) { data, _ := json.Marshal(e); b.Write(data); b.WriteByte('\n') }
	for pkg, tests := range allowedTests {
		for test := range tests {
			if test == "TestMSAllocation_Parity" {
				continue
			}
			path := expectedPaths[pkg+"\x00"+test][0]
			emit(event{Action: "output", Package: module + pkg, Test: test, Output: "open ../../" + path + ": no such file or directory\n"})
			emit(event{Action: "fail", Package: module + pkg, Test: test})
		}
		if pkg == "internal/scanner/nios" {
			emit(event{Action: "fail", Package: module + pkg, Test: "TestMSAllocation_Parity"})
		}
		emit(event{Action: "fail", Package: module + pkg})
	}
	return b.String()
}

func runVerify(t *testing.T, content string, code int) error {
	t.Helper()
	p := filepath.Join(t.TempDir(), "test.json")
	if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return verify(p, code)
}

func TestVerifyAcceptedBaseline(t *testing.T) {
	p := filepath.Join(t.TempDir(), "test.json")
	if err := os.WriteFile(p, []byte(fixtureStream(t)), 0o600); err != nil {
		t.Fatal(err)
	}
	var out strings.Builder
	if err := verifyTo(p, 1, &out); err != nil {
		t.Fatal(err)
	}
	want := "accepted known missing-fixture failures\n" +
		"tests (16): internal/exporter/TestCrossSourceAgreement, internal/exporter/TestNiosServerMetricFieldDrift, internal/scanner/nios/TestMSAllocationUnavailableGolden, internal/scanner/nios/TestMSAllocation_Parity, internal/scanner/nios/TestMSAllocation_Parity/absent, internal/scanner/nios/TestMSAllocation_Parity/both, internal/scanner/nios/TestMSAllocation_Parity/boundary-exact, internal/scanner/nios/TestMSAllocation_Parity/boundary-plus-one, internal/scanner/nios/TestMSAllocation_Parity/dhcp-only, internal/scanner/nios/TestMSAllocation_Parity/dns-only, internal/scanner/nios/TestMSAllocation_Parity/held-back, internal/scanner/nios/TestMSAllocation_Parity/unavailable, internal/scanner/nios/TestMSAllocation_Parity_Adjacency, internal/scanner/nios/TestMSAllocation_Parity_Boundary, internal/scanner/nios/TestMSAllocation_Parity_Distinguishable, server/TestServerNiosServerMetricFieldDrift\n" +
		"paths (14): testdata/cross-source-fixture.json, testdata/ms-allocation/absent.json, testdata/ms-allocation/absent.xml, testdata/ms-allocation/both.json, testdata/ms-allocation/both.xml, testdata/ms-allocation/boundary-exact.json, testdata/ms-allocation/boundary-exact.xml, testdata/ms-allocation/boundary-plus-one.xml, testdata/ms-allocation/dhcp-only.xml, testdata/ms-allocation/dns-only.xml, testdata/ms-allocation/held-back.xml, testdata/ms-allocation/unavailable.json, testdata/ms-allocation/unavailable.xml, testdata/nios-metric-fields.json\n"
	if out.String() != want {
		t.Fatalf("summary mismatch\ngot:  %q\nwant: %q", out.String(), want)
	}
}
func TestVerifyRejectsExitZero(t *testing.T) {
	if runVerify(t, fixtureStream(t), 0) == nil {
		t.Fatal("accepted zero exit")
	}
}
func TestVerifyRejectsMalformedJSON(t *testing.T) {
	if runVerify(t, fixtureStream(t)+"{", 1) == nil {
		t.Fatal("accepted malformed JSON")
	}
}
func TestVerifyRejectsMissingFailure(t *testing.T) {
	var kept strings.Builder
	for _, line := range strings.Split(strings.TrimSpace(fixtureStream(t)), "\n") {
		var e event
		if err := json.Unmarshal([]byte(line), &e); err != nil {
			t.Fatal(err)
		}
		if e.Test == "TestCrossSourceAgreement" {
			continue
		}
		kept.WriteString(line)
		kept.WriteByte('\n')
	}
	if runVerify(t, kept.String(), 1) == nil {
		t.Fatal("accepted missing failure")
	}
}
func TestVerifyRejectsUnexpectedTest(t *testing.T) {
	if runVerify(t, fixtureStream(t)+`{"Action":"fail","Package":"`+module+`server","Test":"TestNewRegression"}`+"\n", 1) == nil {
		t.Fatal("accepted unexpected test")
	}
}
func TestVerifyRejectsUnexpectedPath(t *testing.T) {
	if runVerify(t, strings.Replace(fixtureStream(t), "testdata/nios-metric-fields.json", "testdata/secret.json", 1), 1) == nil {
		t.Fatal("accepted unexpected path")
	}
}
func TestVerifyRejectsSubstitutedAllowlistedPath(t *testing.T) {
	stream := strings.Replace(fixtureStream(t), "testdata/ms-allocation/dns-only.xml", "testdata/ms-allocation/both.xml", 1)
	if runVerify(t, stream, 1) == nil {
		t.Fatal("accepted allowlisted path substituted for required path")
	}
}
func TestVerifyRejectsIncompletePathSet(t *testing.T) {
	stream := strings.Replace(fixtureStream(t), "testdata/ms-allocation/boundary-plus-one.xml", "testdata/ms-allocation/boundary-exact.xml", 1)
	if runVerify(t, stream, 1) == nil {
		t.Fatal("accepted duplicated path instead of required path")
	}
}
func TestVerifyRejectsExtraPreviouslyAllowlistedPath(t *testing.T) {
	stream := strings.Replace(fixtureStream(t), "testdata/ms-allocation/dns-only.xml", "testdata/ms-allocation/dns-only.xml and testdata/ms-allocation/dns-only.json", 1)
	if runVerify(t, stream, 1) == nil {
		t.Fatal("accepted extra path from the former global allowlist")
	}
}
func TestVerifyRejectsNonFixtureDiagnostic(t *testing.T) {
	if runVerify(t, strings.Replace(fixtureStream(t), "no such file or directory", "wrong result", 1), 1) == nil {
		t.Fatal("accepted non-fixture diagnostic")
	}
}
func TestVerifyRejectsMixedPanicInAllowlistedLeaf(t *testing.T) {
	extra, _ := json.Marshal(event{Action: "output", Package: module + "server", Test: "TestServerNiosServerMetricFieldDrift", Output: "panic: synthetic regression\n"})
	if runVerify(t, fixtureStream(t)+string(extra)+"\n", 1) == nil {
		t.Fatal("accepted mixed panic output in allowlisted leaf")
	}
}
func TestVerifyRejectsParentDiagnostic(t *testing.T) {
	extra, _ := json.Marshal(event{Action: "output", Package: module + "internal/scanner/nios", Test: "TestMSAllocation_Parity", Output: "panic: parent regression\n"})
	if runVerify(t, fixtureStream(t)+string(extra)+"\n", 1) == nil {
		t.Fatal("accepted non-fixture diagnostic in parity parent")
	}
}
func TestVerifyRejectsPackageDiagnostic(t *testing.T) {
	extra := `{"Action":"output","Package":"` + module + `server","Output":"build failed unexpectedly\\n"}` + "\n"
	if runVerify(t, fixtureStream(t)+extra, 1) == nil {
		t.Fatal("accepted non-fixture package diagnostic")
	}
}
