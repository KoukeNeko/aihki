//go:build integration

package e2e

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"math/rand/v2"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// The pressure test drives several real accounts at one project at once, with
// no pacing, so that the classification this CLI promises is exercised under
// the contention it was written for rather than in a quiet sequence.
const (
	stressWorkers      = 12
	stressRoundsEach   = 40
	stressBatchSize    = 5
	staleWriteOdds     = 4 // one write in this many deliberately carries a stale version
	exitSuccess        = 0
	exitGeneric        = 1
	exitConflict       = 6
	exitAmbiguousCommi = 11
)

// attempt is one CLI invocation and everything the contract said about it.
type attempt struct {
	worker    int
	operation string
	exitCode  int
	code      string
	message   string
	version   int
	stderr    string
}

func TestConcurrentPressureAgainstDocker(t *testing.T) {
	baseURL := requiredEnv(t, "TAIGA_E2E_URL")
	binary := requiredEnv(t, "TAIGA_E2E_BIN")

	owner := "stress_" + strconv.FormatInt(time.Now().UnixNano(), 36)
	password := "Stress-Password-7fK2mQ9"
	ownerToken := register(t, baseURL, owner, password)
	verifyEmail(t, owner)
	project := createProject(t, baseURL, ownerToken)
	projectSlug := project["slug"].(string)
	projectID := int64(project["id"].(float64))

	workers := make([]stressWorker, 0, stressWorkers)
	for index := range stressWorkers {
		name := fmt.Sprintf("%s_w%d", owner, index)
		token := register(t, baseURL, name, password)
		verifyEmail(t, name)
		addMember(t, baseURL, ownerToken, projectID, name)
		workers = append(workers, stressWorker{index: index, username: name, token: token})
	}

	hot := createIssue(t, baseURL, ownerToken, projectID, "contended issue")
	hotRef := int(hot["ref"].(float64))
	t.Logf("%d workers, %d rounds each, all writing to issue #%d in %s",
		stressWorkers, stressRoundsEach, hotRef, projectSlug)

	results := make([][]attempt, stressWorkers)
	var group sync.WaitGroup
	started := time.Now()
	for index := range workers {
		group.Add(1)
		go func(worker stressWorker) {
			defer group.Done()
			home := t.TempDir()
			env := []string{
				"HOME=" + home,
				"XDG_CONFIG_HOME=" + filepath.Join(home, ".config"),
				"TAIGA_API_URL=" + baseURL,
				"TAIGA_TOKEN=" + worker.token,
				"TAIGA_PROJECT=" + projectSlug,
			}
			results[worker.index] = worker.hammer(binary, env, hotRef)
		}(workers[index])
	}
	group.Wait()
	elapsed := time.Since(started)

	attempts := make([]attempt, 0, stressWorkers*stressRoundsEach)
	for _, batch := range results {
		attempts = append(attempts, batch...)
	}
	report(t, attempts, elapsed)
	assertContractHeldUnderPressure(t, attempts)
	assertNoLostUpdate(t, attempts)
	assertEveryInterestingPathWasReached(t, attempts)
	assertFieldLevelConcurrency(t, attempts)
}

// assertEveryInterestingPathWasReached keeps the checks above from passing
// because nothing exercised them. A run with no rejection proves nothing about
// how a rejection reads.
func assertEveryInterestingPathWasReached(t *testing.T, attempts []attempt) {
	t.Helper()
	seen := map[string]int{}
	for _, result := range attempts {
		if result.code != "" {
			seen[result.code]++
		}
	}
	for _, required := range []string{"occ_conflict", "validation", "not_found"} {
		if seen[required] == 0 {
			t.Errorf("no %s was provoked, so the assertions about it proved nothing", required)
		}
	}
	t.Logf("failure codes reached: %v", seen)
}

// assertFieldLevelConcurrency records the behaviour the README describes.
// Taiga checks the version per field, so writes to a field nobody else is
// touching succeed against the same base version that a contended field
// refuses.
func assertFieldLevelConcurrency(t *testing.T, attempts []attempt) {
	t.Helper()
	var subjectConflicts, descriptionAccepted int
	for _, result := range attempts {
		switch result.operation {
		case "edit--subject":
			if result.code == "occ_conflict" {
				subjectConflicts++
			}
		case "edit--description":
			if result.exitCode == exitSuccess {
				descriptionAccepted++
			}
		}
	}
	if subjectConflicts == 0 {
		t.Error("no worker lost a race on the contended field, so this run had no contention")
	}
	if descriptionAccepted == 0 {
		t.Error("no edit to an uncontended field was accepted, which the per-field check should allow")
	}
	t.Logf("contended field refused %d times; uncontended field accepted %d times",
		subjectConflicts, descriptionAccepted)
}

type stressWorker struct {
	index    int
	username string
	token    string
}

// hammer runs one worker's whole sequence back to back with no pause, mixing
// reads, fresh writes and deliberately stale ones so that conflicts, merges and
// plain successes all occur while the others are doing the same.
func (w stressWorker) hammer(binary string, env []string, hotRef int) []attempt {
	source := rand.New(rand.NewPCG(uint64(w.index), 0x5eed))
	attempts := make([]attempt, 0, stressRoundsEach)
	for round := range stressRoundsEach {
		switch source.IntN(12) {
		case 0, 1, 2:
			attempts = append(attempts, w.contendedEdit(binary, env, hotRef, source))
		case 3:
			// A different field on the same record: Taiga checks the version
			// per field, so this merges where a subject edit would collide.
			attempts = append(attempts, w.editField(binary, env, hotRef, "--description",
				fmt.Sprintf("w%d round %d", w.index, round), source))
		case 4:
			attempts = append(attempts, w.run(binary, env, "comment",
				"--json", "issue", "comment", strconv.Itoa(hotRef), "--body",
				fmt.Sprintf("w%d round %d", w.index, round)))
		case 5:
			attempts = append(attempts, w.run(binary, env, "create",
				"--json", "issue", "create", "--subject",
				fmt.Sprintf("w%d-r%d", w.index, round)))
		case 6:
			attempts = append(attempts, w.run(binary, env, "list", "--json", "issue", "list", "--limit", "20"))
		case 7:
			attempts = append(attempts, w.bulkCreate(binary, env, round))
		case 8:
			// Real sessions contain mistakes. A rejection has to say which
			// field was wrong, not just that something was.
			attempts = append(attempts, w.run(binary, env, "assign-unknown",
				"--json", "issue", "assign", strconv.Itoa(hotRef), "--to", "nobody-"+strconv.Itoa(w.index)))
		case 9:
			attempts = append(attempts, w.run(binary, env, "view-missing",
				"--json", "issue", "view", "999999"))
		case 10:
			attempts = append(attempts, w.run(binary, env, "edit-empty",
				"--json", "issue", "edit", strconv.Itoa(hotRef), "--subject", ""))
		case 11:
			attempts = append(attempts, w.run(binary, env, "assign-self",
				"--json", "issue", "assign", strconv.Itoa(hotRef), "--to", w.username))
		}
	}
	return attempts
}

// contendedEdit writes the subject every worker is writing. Every so often it
// reuses a version it already knows is behind, which is what a script holding a
// record open across a slow decision does.
func (w stressWorker) contendedEdit(binary string, env []string, hotRef int, source *rand.Rand) attempt {
	return w.editField(binary, env, hotRef, "--subject", fmt.Sprintf("owned by w%d", w.index), source)
}

func (w stressWorker) editField(binary string, env []string, hotRef int, flag, value string, source *rand.Rand) attempt {
	view := w.run(binary, env, "view", "--json", "issue", "view", strconv.Itoa(hotRef), "--fields", "subject,version")
	if view.exitCode != exitSuccess {
		return view
	}
	version := view.version
	if source.IntN(staleWriteOdds) == 0 && version > 1 {
		version--
	}
	operation := "edit" + flag
	return w.run(binary, env, operation, "--json", "issue", "edit", strconv.Itoa(hotRef),
		flag, value, "--base-version", strconv.Itoa(version))
}

func (w stressWorker) bulkCreate(binary string, env []string, round int) attempt {
	file := filepath.Join(os.TempDir(), fmt.Sprintf("stress-w%d-r%d.txt", w.index, round))
	subjects := make([]string, 0, stressBatchSize)
	for item := range stressBatchSize {
		subjects = append(subjects, fmt.Sprintf("w%d-r%d-b%d", w.index, round, item))
	}
	if err := os.WriteFile(file, []byte(strings.Join(subjects, "\n")+"\n"), 0o600); err != nil {
		return attempt{worker: w.index, operation: "batch", exitCode: -1, message: err.Error()}
	}
	defer func() { _ = os.Remove(file) }()
	return w.run(binary, env, "batch", "--json", "batch", "create", "issue", file, "--yes")
}

func (w stressWorker) run(binary string, env []string, operation string, args ...string) attempt {
	command := exec.Command(binary, args...)
	command.Env = append(os.Environ(), env...)
	var stdout, stderr bytes.Buffer
	command.Stdout, command.Stderr = &stdout, &stderr

	result := attempt{worker: w.index, operation: operation}
	if err := command.Run(); err != nil {
		var exitErr *exec.ExitError
		if !errors.As(err, &exitErr) {
			result.exitCode, result.message = -1, err.Error()
			return result
		}
		result.exitCode = exitErr.ExitCode()
	}
	result.stderr = strings.TrimSpace(stderr.String())
	if result.exitCode == exitSuccess {
		result.version = versionOf(stdout.Bytes())
		return result
	}
	var failure struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(stderr.Bytes(), &failure); err == nil {
		result.code, result.message = failure.Error.Code, failure.Error.Message
	}
	return result
}

func versionOf(stdout []byte) int {
	var body struct {
		Data struct {
			Version int `json:"version"`
		} `json:"data"`
	}
	if err := json.Unmarshal(stdout, &body); err != nil {
		return 0
	}
	return body.Data.Version
}

// assertContractHeldUnderPressure checks the promises this CLI makes about how
// it names a failure. Contention is expected; being unable to say what happened
// is not.
func assertContractHeldUnderPressure(t *testing.T, attempts []attempt) {
	t.Helper()
	// Bodies that say nothing. A rejection a person cannot act on is the
	// failure this whole error path exists to prevent.
	opaque := map[string]bool{
		"Bad Request": true, "Internal Server Error": true,
		"Not Found": true, "Forbidden": true, "Conflict": true,
	}
	for _, result := range attempts {
		switch {
		case result.exitCode == -1:
			t.Errorf("worker %d %s did not run: %s", result.worker, result.operation, result.message)
		case result.exitCode == exitGeneric || result.code == "internal":
			t.Errorf("worker %d %s reported an internal defect: exit %d code=%q message=%q",
				result.worker, result.operation, result.exitCode, result.code, result.message)
		case result.exitCode == exitAmbiguousCommi:
			t.Errorf("worker %d %s could not confirm its write against a healthy local server: %q",
				result.worker, result.operation, result.message)
		case result.exitCode != exitSuccess && opaque[result.message]:
			t.Errorf("worker %d %s failed with nothing but a status line: exit %d message=%q",
				result.worker, result.operation, result.exitCode, result.message)
		case result.exitCode != exitSuccess && result.code == "":
			t.Errorf("worker %d %s failed without a contract code: exit %d stderr=%q",
				result.worker, result.operation, result.exitCode, result.stderr)
		}
		// A refused write must be named a conflict, because that is what tells
		// a caller to re-read; anything else sends it down a path that cannot
		// succeed.
		if result.exitCode == exitConflict && result.code != "occ_conflict" {
			t.Errorf("worker %d %s exited 6 under code %q", result.worker, result.operation, result.code)
		}
		if result.code == "occ_conflict" && result.exitCode != exitConflict {
			t.Errorf("worker %d %s reported a conflict but exited %d", result.worker, result.operation, result.exitCode)
		}
	}
}

// assertNoLostUpdate is the point of the exercise. Taiga hands back the new
// version with every accepted write, so two accepted writes reporting the same
// version would mean one of them overwrote the other unseen.
func assertNoLostUpdate(t *testing.T, attempts []attempt) {
	t.Helper()
	seen := map[int]attempt{}
	for _, result := range attempts {
		if result.exitCode != exitSuccess || result.version == 0 {
			continue
		}
		if !strings.HasPrefix(result.operation, "edit") && result.operation != "comment" {
			continue
		}
		if previous, clash := seen[result.version]; clash {
			t.Errorf("version %d was returned to worker %d (%s) and worker %d (%s): one write overwrote the other",
				result.version, previous.worker, previous.operation, result.worker, result.operation)
			continue
		}
		seen[result.version] = result
	}
	if len(seen) == 0 {
		t.Fatal("no write was accepted, so this proved nothing")
	}
	t.Logf("%d accepted writes to the contended issue, every one with its own version", len(seen))
}

func report(t *testing.T, attempts []attempt, elapsed time.Duration) {
	t.Helper()
	byOutcome := map[string]int{}
	for _, result := range attempts {
		key := "ok"
		if result.exitCode != exitSuccess {
			key = fmt.Sprintf("exit %d (%s)", result.exitCode, result.code)
		}
		byOutcome[key]++
	}
	keys := make([]string, 0, len(byOutcome))
	for key := range byOutcome {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	t.Logf("%d invocations in %s (%.0f/s)", len(attempts), elapsed.Round(time.Millisecond),
		float64(len(attempts))/elapsed.Seconds())
	for _, key := range keys {
		t.Logf("  %-28s %d", key, byOutcome[key])
	}
}

func addMember(t *testing.T, baseURL, ownerToken string, projectID int64, username string) {
	t.Helper()
	var roles []map[string]any
	apiRequest(t, http.MethodGet, baseURL+"roles?project="+strconv.FormatInt(projectID, 10), ownerToken, nil, &roles)
	if len(roles) == 0 {
		t.Fatal("the project has no roles to add a member to")
	}
	body := map[string]any{
		"project":  projectID,
		"role":     roles[0]["id"],
		"username": username + "@localhost.invalid",
	}
	var membership map[string]any
	apiRequest(t, http.MethodPost, baseURL+"memberships", ownerToken, body, &membership)
}

func createIssue(t *testing.T, baseURL, token string, projectID int64, subject string) map[string]any {
	t.Helper()
	var issue map[string]any
	apiRequest(t, http.MethodPost, baseURL+"issues", token,
		map[string]any{"project": projectID, "subject": subject}, &issue)
	return issue
}
