/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

// Package ci tests the change-control gate script in hack/ci with a fake kubectl.
package ci

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

const (
	earlyWarning = "Warning: [early] [RunAsNonRoot / ISO/IEC 27001 A.8.2] " +
		"app container 'pause' must run as non-root (policy: strict)"
	applied        = "deployment.apps/web configured (server dry run)"
	unrelatedWarn  = "Warning: spec.template.spec.containers[0].image: deprecated"
	workloadDenied = `Error from server (Forbidden): admission webhook "vworkload.kb.io" denied the request: ` +
		`[early] Deployment team-a/web Pod template violates SecurityBaseline: ...`
	podDenied = `Error from server (Forbidden): admission webhook "vpod.kb.io" denied the request: ` +
		`Pod violates SecurityBaseline: ...`
)

// fakeKubectl writes a kubectl that answers the operator readiness check and the
// dry-run apply with the given output and exit status, and records its arguments.
func fakeKubectl(t *testing.T, readyReplicas, applyOutput string, applyStatus int) (path, calls string) {
	t.Helper()
	dir := t.TempDir()
	calls = filepath.Join(dir, "calls")
	script := `#!/usr/bin/env bash
echo "$@" >> "` + calls + `"
case " $* " in
  *" get deployment "*) printf '%s' '` + readyReplicas + `'; [ -n '` + readyReplicas + `' ] && exit 0 || exit 1 ;;
  *" apply "*) cat <<'OUT'
` + applyOutput + `
OUT
    exit ` + string(rune('0'+applyStatus)) + ` ;;
esac
`
	path = filepath.Join(dir, "kubectl")
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil { //nolint:gosec // test helper script
		t.Fatal(err)
	}
	return path, calls
}

// gateScript is the script under test.
var gateScript = filepath.Join("..", "..", "hack", "ci", "check-early-warnings.sh")

func runGate(t *testing.T, kubectl string, args ...string) (output string, exit int) {
	t.Helper()
	// bash opens the script, not this test, so without reading it here `go test` would
	// serve a cached pass after the script changed. Reading it puts it in the cache key.
	if _, err := os.ReadFile(gateScript); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("bash", append([]string{gateScript}, args...)...)
	cmd.Env = append(os.Environ(), "KUBECTL="+kubectl)
	out, err := cmd.CombinedOutput()
	var exitErr *exec.ExitError
	switch {
	case err == nil:
		return string(out), 0
	case errors.As(err, &exitErr):
		return string(out), exitErr.ExitCode()
	default:
		t.Fatal(err)
		return "", -1
	}
}

func TestGateExitCodes(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name        string
		ready       string
		output      string
		status      int
		args        []string
		wantExit    int
		wantContain string
	}{
		{"clean apply passes", "2", applied, 0, []string{"-f", "k8s/"}, 0, "gate: passed (0"},
		{"unrelated warnings are ignored", "2", unrelatedWarn + "\n" + applied, 0,
			[]string{"-f", "k8s/"}, 0, "gate: passed (0"},
		{"an early warning fails the gate", "2", earlyWarning + "\ndeployment.apps/web created (server dry run)", 0,
			[]string{"-f", "k8s/"}, 1, "FAILED, 1 early-validation warning(s), 0 allowed"},
		{"warnings within --allow pass", "2", earlyWarning + "\n" + earlyWarning + "x\ndeployment.apps/web created", 0,
			[]string{"--allow", "2", "-f", "k8s/"}, 0, "gate: passed (2"},
		{"warnings above --allow fail", "2", earlyWarning + "\n" + earlyWarning + "x\ndeployment.apps/web created", 0,
			[]string{"--allow", "1", "-f", "k8s/"}, 1, "FAILED, 2"},
		{"a rejection in enforce mode fails the gate", "2", workloadDenied, 1,
			[]string{"-f", "k8s/"}, 1, "the operator rejected the apply"},
		{"a Pod denial fails the gate", "2", podDenied, 1,
			[]string{"-f", "k8s/"}, 1, "the operator rejected the apply"},
		{"a kubectl failure unrelated to policy is not a violation", "2",
			"error: the server could not find the requested resource", 1, []string{"-f", "k8s/"}, 2, "unrelated to policy"},
		{"an operator with no ready replica cannot vouch", "0", "", 0, []string{"-f", "k8s/"}, 3, "no ready replica"},
		{"an unreadable operator deployment cannot vouch either", "", "", 0, []string{"-f", "k8s/"}, 3, "cannot be read"},
		{"the check can be skipped explicitly", "", "deployment.apps/web configured", 0,
			[]string{"--no-operator-check", "-f", "k8s/"}, 0, "gate: passed"},
		{"nothing to apply is a usage error", "2", "", 0, nil, 2, "nothing to apply"},
		{"--allow needs a number", "2", "", 0, []string{"--allow", "many", "-f", "k8s/"}, 2, "needs a number"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			kubectl, _ := fakeKubectl(t, c.ready, c.output, c.status)
			out, exit := runGate(t, kubectl, c.args...)
			if exit != c.wantExit {
				t.Fatalf("exit = %d, want %d\n%s", exit, c.wantExit, out)
			}
			if !strings.Contains(out, c.wantContain) {
				t.Fatalf("output does not contain %q:\n%s", c.wantContain, out)
			}
		})
	}
}

func TestGateShowsTheOperatorsOutputAndPassesArgumentsThrough(t *testing.T) {
	t.Parallel()
	kubectl, calls := fakeKubectl(t, "2", earlyWarning+"\ndeployment.apps/web created (server dry run)", 0)

	out, exit := runGate(t, kubectl, "--context", "staging", "-f", "k8s/", "-n", "team-a")
	if exit != 1 || !strings.Contains(out, "must run as non-root") {
		t.Fatalf("the warning text must reach the pipeline log, exit %d:\n%s", exit, out)
	}
	recorded, err := os.ReadFile(calls)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"--context staging -n platform-governance-operator-system get deployment",
		"--context staging apply --dry-run=server -f k8s/ -n team-a",
	} {
		if !strings.Contains(string(recorded), want) {
			t.Errorf("kubectl was not called with %q; calls:\n%s", want, recorded)
		}
	}
}
