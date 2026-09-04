package verify

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func fake(t *testing.T) (docker, composeFile string) {
	t.Helper()
	abs, _ := filepath.Abs("testdata/bin/docker")
	t.Setenv("FAKE_DOCKER_LOG", filepath.Join(t.TempDir(), "log"))
	cf := filepath.Join(t.TempDir(), "compose.yaml")
	os.WriteFile(cf, []byte("services: {verify: {image: x}}\n"), 0o644)
	return abs, cf
}

func logLines(t *testing.T) []string {
	b, _ := os.ReadFile(os.Getenv("FAKE_DOCKER_LOG"))
	return strings.Split(strings.TrimSpace(string(b)), "\n")
}

func TestRunPass(t *testing.T) {
	docker, cf := fake(t)
	r := &ComposeRunner{Docker: docker}
	res, err := r.Run(context.Background(), Spec{ComposeFile: cf, Service: "verify", ProjectName: "cmoa-t-r-c", CandidateDir: "/cand"})
	if err != nil {
		t.Fatal(err)
	}
	if res.ExitCode != 0 || res.TimedOut || string(res.Stdout) != "test output\n" || !strings.Contains(string(res.Stderr), "warning") {
		t.Fatalf("%+v", res)
	}
	lines := logLines(t)
	if len(lines) != 3 {
		t.Fatalf("want run + down + ps, got %v", lines)
	}
	want := "compose -f " + cf + " -p cmoa-t-r-c run --rm --no-deps -T --quiet-pull verify [/cand]"
	if lines[0] != want {
		t.Fatalf("run argv:\n got %s\nwant %s", lines[0], want)
	}
	if !strings.HasPrefix(lines[1], "compose -f "+cf+" -p cmoa-t-r-c down -v --remove-orphans") {
		t.Fatalf("down argv: %s", lines[1])
	}
	if res.Command[0] != docker || res.Command[len(res.Command)-1] != "verify" {
		t.Fatalf("command = %v", res.Command)
	}
}

func TestRunFail(t *testing.T) {
	docker, cf := fake(t)
	t.Setenv("FAKE_DOCKER_EXIT", "3")
	res, err := (&ComposeRunner{Docker: docker}).Run(context.Background(), Spec{ComposeFile: cf, Service: "verify", ProjectName: "p", CandidateDir: "/c"})
	if err != nil || res.ExitCode != 3 {
		t.Fatalf("res=%+v err=%v", res, err)
	}
	if len(logLines(t)) != 3 {
		t.Fatal("down must run after a failing candidate")
	}
}

func TestRunTimeout(t *testing.T) {
	docker, cf := fake(t)
	t.Setenv("FAKE_DOCKER_SLEEP", "5")
	start := time.Now()
	res, err := (&ComposeRunner{Docker: docker, KillAfter: 300 * time.Millisecond}).Run(context.Background(), Spec{ComposeFile: cf, Service: "verify", ProjectName: "p", CandidateDir: "/c", Timeout: 200 * time.Millisecond})
	if err != nil || !res.TimedOut {
		t.Fatalf("res=%+v err=%v", res, err)
	}
	if time.Since(start) > 3*time.Second {
		t.Fatal("timeout did not interrupt")
	}
	if len(logLines(t)) != 3 {
		t.Fatal("down must run after a timeout")
	}
}

func TestRunnerErrors(t *testing.T) {
	_, cf := fake(t)
	ctx := context.Background()
	_, err := (&ComposeRunner{Docker: "/no/such/docker"}).Run(ctx, Spec{ComposeFile: cf, Service: "v", ProjectName: "p"})
	if _, ok := errors.AsType[*RunnerError](err); !ok {
		t.Fatalf("missing docker: %v", err)
	}
	docker, _ := fake(t)
	_, err = (&ComposeRunner{Docker: docker}).Run(ctx, Spec{ComposeFile: "/nope.yaml", Service: "v", ProjectName: "p"})
	if _, ok := errors.AsType[*RunnerError](err); !ok {
		t.Fatalf("missing compose: %v", err)
	}
	_, err = (&ComposeRunner{Docker: docker}).Run(ctx, Spec{ComposeFile: cf, Service: "v", ProjectName: "Bad Name"})
	if _, ok := errors.AsType[*RunnerError](err); !ok {
		t.Fatalf("bad project: %v", err)
	}
}

func TestProjectName(t *testing.T) {
	got := ProjectName("hello-fix", "20260904T120000Z-0a0b0c0d", "Qwen")
	if !projectPattern.MatchString(got) || got != "cmoa-hello-fix-20260904t120000z-0a0b0c0d-qwen" {
		t.Fatalf("got %q", got)
	}
}

func TestLeftoverContainerIsRemoved(t *testing.T) {
	docker, cf := fake(t)
	t.Setenv("FAKE_DOCKER_LEFTOVER", "1")
	if _, err := (&ComposeRunner{Docker: docker}).Run(context.Background(), Spec{ComposeFile: cf, Service: "verify", ProjectName: "p", CandidateDir: "/c"}); err != nil {
		t.Fatal(err)
	}
	lines := logLines(t)
	if len(lines) != 4 || !strings.HasPrefix(lines[2], "ps -aq --filter label=com.docker.compose.project=p") || !strings.HasPrefix(lines[3], "rm -f abc123") {
		t.Fatalf("expected ps + rm -f after down, got %v", lines)
	}
}
