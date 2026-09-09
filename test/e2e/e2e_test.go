//go:build integration

package e2e

import (
	"archive/zip"
	"bytes"
	"compress/gzip"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

type envelope struct {
	Data  map[string]any   `json:"data"`
	Items []map[string]any `json:"items"`
	Plan  map[string]any   `json:"plan"`
	Page  map[string]any   `json:"page"`
}

func TestPhaseOneAgainstDocker(t *testing.T) {
	baseURL := requiredEnv(t, "TAIGA_E2E_URL")
	host := requiredEnv(t, "TAIGA_E2E_HOST")
	binary := requiredEnv(t, "TAIGA_E2E_BIN")
	home := t.TempDir()
	username := "e2e_" + strconv.FormatInt(time.Now().UnixNano(), 36)
	password := "E2E-Password-7fK2mQ9"
	token := register(t, baseURL, username, password)
	verifyEmail(t, username)
	memberUsername := "member_" + strconv.FormatInt(time.Now().UnixNano(), 36)
	memberEmail := memberUsername + "@localhost.invalid"
	memberToken := register(t, baseURL, memberUsername, password)
	verifyEmail(t, memberUsername)
	project := createProject(t, baseURL, token)
	projectID := int64(project["id"].(float64))
	projectSlug := project["slug"].(string)
	secondaryProject := createProject(t, baseURL, token)
	secondaryProjectID := int64(secondaryProject["id"].(float64))
	secondaryProjectSlug := secondaryProject["slug"].(string)
	t.Cleanup(func() {
		deleteProject(t, baseURL, token, projectID)
		deleteProject(t, baseURL, token, secondaryProjectID)
	})

	runner := cliRunner{t: t, binary: binary, dir: t.TempDir(), env: []string{
		"HOME=" + home,
		"XDG_CONFIG_HOME=" + filepath.Join(home, ".config"),
		"TAIGA_API_URL=" + baseURL,
		"TAIGA_TOKEN=" + token,
		"TAIGA_PROJECT=" + projectSlug,
	}}

	runner.jsonOK("doctor", "--url", host)
	diagnosticPath := filepath.Join(runner.dir, "taiga-diagnostics.zip")
	diagnostic := runner.jsonOK("doctor", "bundle", diagnosticPath)
	if diagnostic.Data["redacted"] != true || diagnostic.Data["uploaded"] != false {
		t.Fatalf("diagnostic bundle result=%#v", diagnostic.Data)
	}
	diagnosticContents := readDiagnosticZip(t, diagnosticPath)
	for _, forbidden := range []string{token, username, projectSlug, baseURL, host, runner.dir, home} {
		if bytes.Contains(diagnosticContents, []byte(forbidden)) {
			t.Fatalf("diagnostic bundle leaked %q: %s", forbidden, diagnosticContents)
		}
	}
	runner.jsonOK("auth", "status")
	runner.jsonOK("project", "list", "--limit", "10")
	runner.jsonOK("project", "view", projectSlug)
	runner.jsonOK("project", "use", projectSlug)
	runner.jsonOK("schema", "issue", "view")
	projectTemplate := firstProjectTemplate(t, baseURL, token)
	managedProject := runner.jsonOK("project", "create", "--name", "Managed "+username, "--description", "created by CLI E2E", "--template", projectTemplate)
	managedProjectSlug := managedProject.Data["slug"].(string)
	managedProjectDryRun := runner.jsonOK("project", "edit", managedProjectSlug, "--description", "must not persist", "--dry-run")
	if managedProjectDryRun.Plan["performed"] != false || managedProjectDryRun.Plan["would_write"] != true {
		t.Fatalf("project dry-run plan=%#v", managedProjectDryRun.Plan)
	}
	managedProjectEdited := runner.jsonOK("project", "edit", managedProjectSlug, "--description", "updated by CLI E2E", "--wiki=false")
	if managedProjectEdited.Data["description"] != "updated by CLI E2E" || managedProjectEdited.Data["is_wiki_activated"] != false {
		t.Fatalf("project edit=%#v", managedProjectEdited.Data)
	}
	stdout, stderr, code := runner.run("--json", "project", "archive", managedProjectSlug)
	if code != 7 || strings.TrimSpace(stdout) != "" || !strings.Contains(stderr, "unsupported_capability") {
		t.Fatalf("project archive capability exit=%d stdout=%s stderr=%s", code, stdout, stderr)
	}
	stdout, stderr, code = runner.run("--json", "--no-input", "project", "delete", managedProjectSlug)
	if code != 10 || strings.TrimSpace(stdout) != "" {
		t.Fatalf("unconfirmed project delete exit=%d stdout=%s stderr=%s", code, stdout, stderr)
	}
	deletedProject := runner.jsonOK("project", "delete", managedProjectSlug, "--yes")
	if deletedProject.Data["deletion_requested"] != true || deletedProject.Data["asynchronous"] != true {
		t.Fatalf("project deletion request=%#v", deletedProject.Data)
	}
	role := runner.jsonOK("role", "create", "--name", "E2E Reviewer", "--computable=false")
	roleSlug := role.Data["slug"].(string)
	roleEdited := runner.jsonOK("role", "edit", roleSlug, "--name", "E2E Review", "--order", "30")
	if roleEdited.Data["name"] != "E2E Review" || roleEdited.Data["order"] != float64(30) {
		t.Fatalf("role edit=%#v", roleEdited.Data)
	}
	member := runner.jsonOK("member", "add", memberEmail, "--role", roleSlug)
	membershipID := int64(member.Data["id"].(float64))
	if member.Data["user_email"] != memberEmail || member.Data["role_name"] != "E2E Review" {
		t.Fatalf("member add=%#v", member.Data)
	}
	memberEdited := runner.jsonOK("member", "edit", strconv.FormatInt(membershipID, 10), "--admin=true")
	if memberEdited.Data["is_admin"] != true {
		t.Fatalf("member admin edit=%#v", memberEdited.Data)
	}
	members := runner.jsonOK("member", "list", "--fields", "id,user_email,role_name,is_admin")
	if !containsMembership(members.Items, membershipID, memberEmail) {
		t.Fatalf("membership missing from list: %#v", members.Items)
	}
	stdout, stderr, code = runner.run("--json", "--no-input", "member", "remove", strconv.FormatInt(membershipID, 10))
	if code != 10 || strings.TrimSpace(stdout) != "" {
		t.Fatalf("unconfirmed member removal exit=%d stdout=%s stderr=%s", code, stdout, stderr)
	}
	removedMember := runner.jsonOK("member", "remove", strconv.FormatInt(membershipID, 10), "--yes")
	if removedMember.Data["removed"] != true {
		t.Fatalf("member removal=%#v", removedMember.Data)
	}
	deletedRole := runner.jsonOK("role", "delete", roleSlug, "--yes")
	if deletedRole.Data["deleted"] != true {
		t.Fatalf("role deletion=%#v", deletedRole.Data)
	}
	webhookSecret := "E2E-webhook-secret-must-not-leak"
	webhook := runner.jsonOK("webhook", "create", "--name", "E2E Hook", "--url", "http://webhook.invalid/taiga", "--secret", webhookSecret)
	webhookID := int64(webhook.Data["id"].(float64))
	webhookJSON, _ := json.Marshal(webhook)
	if bytes.Contains(webhookJSON, []byte(webhookSecret)) {
		t.Fatalf("webhook secret leaked: %s", webhookJSON)
	}
	webhooks := runner.jsonOK("webhook", "list", "--fields", "id,name,url,logs_counter")
	if !containsID(webhooks.Items, webhookID) {
		t.Fatalf("webhook missing from list: %#v", webhooks.Items)
	}
	runner.jsonOK("webhook", "view", strconv.FormatInt(webhookID, 10), "--fields", "id,name,url,logs_counter")
	webhookEdited := runner.jsonOK("webhook", "edit", strconv.FormatInt(webhookID, 10), "--name", "E2E Hook Updated")
	if webhookEdited.Data["name"] != "E2E Hook Updated" {
		t.Fatalf("webhook edit=%#v", webhookEdited.Data)
	}
	webhookTest := runner.jsonOK("webhook", "test", strconv.FormatInt(webhookID, 10), "--fields", "id,webhook,status,duration,response_data")
	if webhookTest.Data["webhook"] != float64(webhookID) {
		t.Fatalf("webhook test=%#v", webhookTest.Data)
	}
	stdout, stderr, code = runner.run("--json", "--no-input", "webhook", "delete", strconv.FormatInt(webhookID, 10))
	if code != 10 || strings.TrimSpace(stdout) != "" {
		t.Fatalf("unconfirmed webhook deletion exit=%d stdout=%s stderr=%s", code, stdout, stderr)
	}
	deletedWebhook := runner.jsonOK("webhook", "delete", strconv.FormatInt(webhookID, 10), "--yes")
	if deletedWebhook.Data["deleted"] != true {
		t.Fatalf("webhook deletion=%#v", deletedWebhook.Data)
	}

	created := runner.jsonOK("issue", "create", "--subject", "E2E issue", "--description", "created by integration test")
	ref := int(created.Data["ref"].(float64))
	version := int(created.Data["version"].(float64))
	target := strconv.Itoa(ref)
	customField := runner.jsonOK("custom-field", "create", "issue", "--name", "Environment", "--type", "dropdown", "--option", "staging", "--option", "production")
	customFieldID := int64(customField.Data["id"].(float64))
	customFieldEdited := runner.jsonOK("custom-field", "edit", "issue", strconv.FormatInt(customFieldID, 10), "--description", "Deployment environment")
	if customFieldEdited.Data["description"] != "Deployment environment" {
		t.Fatalf("custom field edit=%#v", customFieldEdited.Data)
	}
	customValues := runner.jsonOK("custom-field", "set", "issue", target, "--value", `Environment="staging"`)
	resolvedValues := customValues.Data["values"].(map[string]any)
	if resolvedValues["Environment"] != "staging" {
		t.Fatalf("custom field set=%#v", customValues.Data)
	}
	customValues = runner.jsonOK("custom-field", "values", "issue", target)
	resolvedValues = customValues.Data["values"].(map[string]any)
	if resolvedValues["Environment"] != "staging" {
		t.Fatalf("custom field readback=%#v", customValues.Data)
	}
	stdout, stderr, code = runner.run("--json", "--no-input", "custom-field", "delete", "issue", strconv.FormatInt(customFieldID, 10))
	if code != 10 || strings.TrimSpace(stdout) != "" {
		t.Fatalf("unconfirmed custom field delete exit=%d stdout=%s stderr=%s", code, stdout, stderr)
	}
	deletedCustomField := runner.jsonOK("custom-field", "delete", "issue", strconv.FormatInt(customFieldID, 10), "--yes")
	if deletedCustomField.Data["deleted"] != true {
		t.Fatalf("custom field deletion=%#v", deletedCustomField.Data)
	}

	listed := runner.jsonOK("issue", "list", "--fields", "ref,subject,status,version")
	if !containsRef(listed.Items, ref) {
		t.Fatalf("created issue ref %d missing from list", ref)
	}
	runner.jsonOK("issue", "view", target, "--fields", "ref,subject,version")

	dryRun := runner.jsonOK("issue", "edit", target, "--subject", "must not persist", "--dry-run")
	if dryRun.Plan["performed"] != false || dryRun.Plan["would_write"] != true {
		t.Fatalf("dry-run plan = %#v", dryRun.Plan)
	}
	view := runner.jsonOK("issue", "view", target, "--fields", "subject")
	if view.Data["subject"] != "E2E issue" {
		t.Fatalf("dry-run mutated issue: %#v", view.Data)
	}

	edited := runner.jsonOK("issue", "edit", target, "--subject", "E2E issue updated", "--base-version", strconv.Itoa(version))
	version = int(edited.Data["version"].(float64))
	issueID := int64(edited.Data["id"].(float64))
	var externalUpdate map[string]any
	apiRequest(t, http.MethodPatch, baseURL+"issues/"+strconv.FormatInt(issueID, 10), token, map[string]any{"subject": "external concurrent edit", "version": version}, &externalUpdate)
	externalVersion := int(externalUpdate["version"].(float64))
	stdout, stderr, code = runner.run("--json", "issue", "edit", target, "--subject", "must conflict", "--base-version", strconv.Itoa(version))
	if code != 6 || strings.TrimSpace(stdout) != "" {
		t.Fatalf("stale OCC edit exit=%d stdout=%s stderr=%s", code, stdout, stderr)
	}
	var conflictEnvelope map[string]any
	if err := json.Unmarshal([]byte(stderr), &conflictEnvelope); err != nil || conflictEnvelope["error"].(map[string]any)["code"] != "occ_conflict" {
		t.Fatalf("invalid OCC error: %v: %s", err, stderr)
	}
	afterConflict := runner.jsonOK("issue", "view", target, "--fields", "subject,version")
	if afterConflict.Data["subject"] != "external concurrent edit" {
		t.Fatalf("stale edit overwrote concurrent change: %#v", afterConflict.Data)
	}
	version = externalVersion
	assigned := runner.jsonOK("issue", "assign", target, "--to", username, "--base-version", strconv.Itoa(version))
	version = int(assigned.Data["version"].(float64))
	commented := runner.jsonOK("issue", "comment", target, "--body", "integration comment", "--base-version", strconv.Itoa(version))
	version = int(commented.Data["version"].(float64))
	watchedIssue := runner.jsonOK("issue", "watch", target)
	if watchedIssue.Data["watching"] != true || watchedIssue.Data["verified"] != true {
		t.Fatalf("issue watch was not verified: %#v", watchedIssue.Data)
	}
	issueWatchers := runner.jsonOK("issue", "watchers", target, "--fields", "id,username,full_name")
	if !containsUsername(issueWatchers.Items, username) {
		t.Fatalf("issue watcher list missing %q: %#v", username, issueWatchers.Items)
	}
	issueView := runner.jsonOK("issue", "view", target, "--fields", "ref,is_watcher")
	if issueView.Data["is_watcher"] != true {
		t.Fatalf("issue view did not confirm watch state: %#v", issueView.Data)
	}
	issueHistory := runner.jsonOK("issue", "history", target, "--type", "comment", "--fields", "id,kind,author,comment")
	if !containsComment(issueHistory.Items, "integration comment") {
		t.Fatalf("issue comment missing from CLI history: %#v", issueHistory.Items)
	}
	issueCommentID := historyIDForComment(t, issueHistory.Items, "integration comment")
	editedIssueComment := runner.jsonOK("comment", "edit", "issue", target, issueCommentID, "--body", "integration comment edited")
	if editedIssueComment.Data["verified"] != true {
		t.Fatalf("edited issue comment=%#v", editedIssueComment.Data)
	}
	deletedIssueComment := runner.jsonOK("comment", "delete", "issue", target, issueCommentID, "--yes")
	if deletedIssueComment.Data["verified"] != true {
		t.Fatalf("deleted issue comment=%#v", deletedIssueComment.Data)
	}
	commentVersions := runner.jsonOK("comment", "versions", "issue", target, issueCommentID)
	if len(commentVersions.Items) == 0 {
		t.Fatalf("comment versions=%#v", commentVersions.Items)
	}
	restoredIssueComment := runner.jsonOK("comment", "undelete", "issue", target, issueCommentID)
	if restoredIssueComment.Data["verified"] != true {
		t.Fatalf("restored issue comment=%#v", restoredIssueComment.Data)
	}
	unwatchedIssue := runner.jsonOK("issue", "unwatch", target)
	if unwatchedIssue.Data["watching"] != false || unwatchedIssue.Data["verified"] != true {
		t.Fatalf("issue unwatch was not verified: %#v", unwatchedIssue.Data)
	}
	votedIssue := runner.jsonOK("issue", "vote", target)
	if votedIssue.Data["voting"] != true || votedIssue.Data["verified"] != true {
		t.Fatalf("issue vote was not verified: %#v", votedIssue.Data)
	}
	issueVoters := runner.jsonOK("issue", "voters", target, "--fields", "id,username,full_name")
	if !containsUsername(issueVoters.Items, username) {
		t.Fatalf("issue voter list missing %q: %#v", username, issueVoters.Items)
	}
	unvotedIssue := runner.jsonOK("issue", "unvote", target)
	if unvotedIssue.Data["voting"] != false || unvotedIssue.Data["verified"] != true {
		t.Fatalf("issue unvote was not verified: %#v", unvotedIssue.Data)
	}
	closedStatus := firstClosedStatus(t, baseURL, token, projectID)
	closed := runner.jsonOK("issue", "close", target, "--status", closedStatus, "--base-version", strconv.Itoa(version))
	if closed.Data["is_closed"] != true {
		t.Fatalf("issue not closed: %#v", closed.Data)
	}
	attachmentPath := filepath.Join(runner.dir, "evidence.txt")
	if err := os.WriteFile(attachmentPath, []byte("Taiga CLI attachment evidence"), 0o600); err != nil {
		t.Fatal(err)
	}
	attachment := runner.jsonOK("attachment", "add", "issue", target, attachmentPath, "--description", "E2E evidence")
	attachmentID := int64(attachment.Data["id"].(float64))
	attachmentList := runner.jsonOK("attachment", "list", "issue", target, "--fields", "id,name,size,description")
	if !containsID(attachmentList.Items, attachmentID) {
		t.Fatalf("attachment %d missing from list", attachmentID)
	}
	runner.jsonOK("attachment", "view", "issue", strconv.FormatInt(attachmentID, 10), "--fields", "id,name,size,url,sha1")
	downloadPath := filepath.Join(runner.dir, "downloaded-evidence.txt")
	downloadedAttachment := runner.jsonOK("attachment", "download", "issue", strconv.FormatInt(attachmentID, 10), "--output", downloadPath)
	if downloadedAttachment.Data["verified"] != true || downloadedAttachment.Data["bytes"] != float64(len("Taiga CLI attachment evidence")) {
		t.Fatalf("attachment download=%#v", downloadedAttachment.Data)
	}
	downloadedData, err := os.ReadFile(downloadPath)
	if err != nil || string(downloadedData) != "Taiga CLI attachment evidence" {
		t.Fatalf("downloaded attachment data=%q err=%v", downloadedData, err)
	}
	editedAttachment := runner.jsonOK("attachment", "edit", "issue", strconv.FormatInt(attachmentID, 10), "--description", "Archived evidence", "--deprecated")
	if editedAttachment.Data["description"] != "Archived evidence" || editedAttachment.Data["is_deprecated"] != true {
		t.Fatalf("attachment metadata not updated: %#v", editedAttachment.Data)
	}
	stdout, stderr, code = runner.run("--json", "--no-input", "attachment", "delete", "issue", strconv.FormatInt(attachmentID, 10))
	if code != 10 || strings.TrimSpace(stdout) != "" {
		t.Fatalf("unconfirmed delete exit=%d stdout=%s stderr=%s", code, stdout, stderr)
	}
	deletedAttachment := runner.jsonOK("attachment", "delete", "issue", strconv.FormatInt(attachmentID, 10), "--yes")
	if deletedAttachment.Data["deleted"] != true {
		t.Fatalf("attachment not deleted: %#v", deletedAttachment.Data)
	}
	attachmentList = runner.jsonOK("attachment", "list", "issue", target, "--fields", "id,name")
	if containsID(attachmentList.Items, attachmentID) {
		t.Fatalf("deleted attachment %d remains in list", attachmentID)
	}

	sprint := runner.jsonOK("sprint", "create", "--name", "E2E Sprint", "--start", "2026-08-31", "--finish", "2026-09-07")
	milestoneSlug := sprint.Data["slug"].(string)
	runner.jsonOK("sprint", "view", milestoneSlug, "--fields", "name,slug,start,finish,closed")
	openSprints := runner.jsonOK("sprint", "list", "--state", "open", "--fields", "name,slug,start,finish,closed")
	if !containsSlug(openSprints.Items, milestoneSlug) {
		t.Fatalf("created sprint %q missing from open list", milestoneSlug)
	}
	sprintDryRun := runner.jsonOK("sprint", "edit", milestoneSlug, "--finish", "2026-09-08", "--dry-run")
	if sprintDryRun.Plan["performed"] != false || sprintDryRun.Plan["would_write"] != true {
		t.Fatalf("sprint dry-run plan = %#v", sprintDryRun.Plan)
	}
	story := runner.jsonOK("story", "create", "--subject", "E2E story", "--description", "created by integration test")
	storyRef := int(story.Data["ref"].(float64))
	storyID := int64(story.Data["id"].(float64))
	storyVersion := int(story.Data["version"].(float64))
	storyTarget := strconv.Itoa(storyRef)
	stories := runner.jsonOK("story", "list", "--order-by", "subject", "--fields", "ref,subject,status,version")
	if !containsRef(stories.Items, storyRef) {
		t.Fatalf("created story ref %d missing from list", storyRef)
	}
	runner.jsonOK("story", "view", storyTarget, "--fields", "ref,subject,version,sprint_slug")
	storyDryRun := runner.jsonOK("story", "edit", storyTarget, "--subject", "must not persist", "--dry-run")
	if storyDryRun.Plan["performed"] != false || storyDryRun.Plan["would_write"] != true {
		t.Fatalf("story dry-run plan = %#v", storyDryRun.Plan)
	}
	storyView := runner.jsonOK("story", "view", storyTarget, "--fields", "subject")
	if storyView.Data["subject"] != "E2E story" {
		t.Fatalf("story dry-run mutated state: %#v", storyView.Data)
	}
	storyEdited := runner.jsonOK("story", "edit", storyTarget, "--subject", "E2E story updated", "--base-version", strconv.Itoa(storyVersion))
	storyVersion = int(storyEdited.Data["version"].(float64))
	var externalStoryUpdate map[string]any
	apiRequest(t, http.MethodPatch, baseURL+"userstories/"+strconv.FormatInt(storyID, 10), token, map[string]any{"subject": "external story edit", "version": storyVersion}, &externalStoryUpdate)
	externalStoryVersion := int(externalStoryUpdate["version"].(float64))
	stdout, stderr, code = runner.run("--json", "story", "edit", storyTarget, "--subject", "must conflict", "--base-version", strconv.Itoa(storyVersion))
	if code != 6 || strings.TrimSpace(stdout) != "" {
		t.Fatalf("stale story OCC edit exit=%d stdout=%s stderr=%s", code, stdout, stderr)
	}
	staleDifferentField := runner.jsonOK("story", "edit", storyTarget, "--description", "field-aware stale merge", "--base-version", strconv.Itoa(storyVersion))
	storyVersion = int(staleDifferentField.Data["version"].(float64))
	if storyVersion <= externalStoryVersion {
		t.Fatalf("field-aware stale update did not advance version: %#v", staleDifferentField.Data)
	}
	storyAssigned := runner.jsonOK("story", "assign", storyTarget, "--to", username, "--base-version", strconv.Itoa(storyVersion))
	storyVersion = int(storyAssigned.Data["version"].(float64))
	assignedUsers := storyAssigned.Data["assigned_users"].([]any)
	if len(assignedUsers) != 1 {
		t.Fatalf("story assigned_users = %#v", assignedUsers)
	}
	storyMoved := runner.jsonOK("story", "move", storyTarget, "--sprint", milestoneSlug, "--base-version", strconv.Itoa(storyVersion))
	storyVersion = int(storyMoved.Data["version"].(float64))
	if storyMoved.Data["sprint_slug"] != milestoneSlug {
		t.Fatalf("story sprint = %#v", storyMoved.Data)
	}
	sprintStories := runner.jsonOK("story", "list", "--sprint", milestoneSlug, "--fields", "ref,subject,sprint_slug")
	if !containsRef(sprintStories.Items, storyRef) {
		t.Fatalf("story missing from sprint-filtered list")
	}
	storyBacklog := runner.jsonOK("story", "move", storyTarget, "--sprint", "backlog", "--base-version", strconv.Itoa(storyVersion))
	storyVersion = int(storyBacklog.Data["version"].(float64))
	if value, ok := storyBacklog.Data["sprint_slug"]; ok && value != "" {
		t.Fatalf("story did not return to backlog: %#v", storyBacklog.Data)
	}
	backlogStories := runner.jsonOK("story", "list", "--sprint", "backlog", "--fields", "ref,subject,sprint_slug")
	if !containsRef(backlogStories.Items, storyRef) {
		t.Fatalf("story missing from backlog-filtered list")
	}
	storyComment := runner.jsonOK("story", "comment", storyTarget, "--body", "story integration comment", "--base-version", strconv.Itoa(storyVersion))
	storyVersion = int(storyComment.Data["version"].(float64))
	watchedStory := runner.jsonOK("story", "watch", storyTarget)
	if watchedStory.Data["watching"] != true || watchedStory.Data["verified"] != true {
		t.Fatalf("story watch was not verified: %#v", watchedStory.Data)
	}
	storyHistory := runner.jsonOK("story", "history", storyTarget, "--type", "comment", "--fields", "id,kind,author,comment")
	if !containsComment(storyHistory.Items, "story integration comment") {
		t.Fatalf("story comment missing from CLI history: %#v", storyHistory.Items)
	}
	unwatchedStory := runner.jsonOK("story", "unwatch", storyTarget)
	if unwatchedStory.Data["watching"] != false || unwatchedStory.Data["verified"] != true {
		t.Fatalf("story unwatch was not verified: %#v", unwatchedStory.Data)
	}
	votedStory := runner.jsonOK("story", "vote", storyTarget)
	if votedStory.Data["voting"] != true || votedStory.Data["verified"] != true {
		t.Fatalf("story vote was not verified: %#v", votedStory.Data)
	}
	unvotedStory := runner.jsonOK("story", "unvote", storyTarget)
	if unvotedStory.Data["voting"] != false || unvotedStory.Data["verified"] != true {
		t.Fatalf("story unvote was not verified: %#v", unvotedStory.Data)
	}
	closedStoryStatus := firstClosedStoryStatus(t, baseURL, token, projectID)
	closedStory := runner.jsonOK("story", "close", storyTarget, "--status", closedStoryStatus, "--base-version", strconv.Itoa(storyVersion))
	if closedStory.Data["is_closed"] != true {
		t.Fatalf("story not closed: %#v", closedStory.Data)
	}

	secondaryStory := runner.jsonOK("--project", secondaryProjectSlug, "story", "create", "--subject", "Cross-project story")
	secondaryStoryRef := int(secondaryStory.Data["ref"].(float64))
	secondaryStoryTarget := secondaryProjectSlug + "#" + strconv.Itoa(secondaryStoryRef)
	epic := runner.jsonOK("epic", "create", "--subject", "E2E epic", "--description", "cross-project relation test")
	epicRef := int(epic.Data["ref"].(float64))
	epicID := int64(epic.Data["id"].(float64))
	epicVersion := int(epic.Data["version"].(float64))
	epicTarget := strconv.Itoa(epicRef)
	epics := runner.jsonOK("epic", "list", "--fields", "ref,subject,status,version")
	if !containsRef(epics.Items, epicRef) {
		t.Fatalf("created epic ref %d missing from list", epicRef)
	}
	runner.jsonOK("epic", "view", epicTarget, "--fields", "ref,subject,status,version")
	epicDryRun := runner.jsonOK("epic", "edit", epicTarget, "--subject", "must not persist", "--dry-run")
	if epicDryRun.Plan["performed"] != false || epicDryRun.Plan["would_write"] != true {
		t.Fatalf("epic dry-run plan=%#v", epicDryRun.Plan)
	}
	epicEdited := runner.jsonOK("epic", "edit", epicTarget, "--subject", "E2E epic updated", "--base-version", strconv.Itoa(epicVersion))
	epicVersion = int(epicEdited.Data["version"].(float64))
	epicAttachment := runner.jsonOK("attachment", "add", "epic", epicTarget, attachmentPath, "--description", "Epic evidence")
	epicAttachmentID := int64(epicAttachment.Data["id"].(float64))
	epicAttachments := runner.jsonOK("attachment", "list", "epic", epicTarget, "--fields", "id,name,description")
	if !containsID(epicAttachments.Items, epicAttachmentID) {
		t.Fatalf("epic attachment %d missing from list", epicAttachmentID)
	}
	deletedEpicAttachment := runner.jsonOK("attachment", "delete", "epic", strconv.FormatInt(epicAttachmentID, 10), "--yes")
	if deletedEpicAttachment.Data["deleted"] != true {
		t.Fatalf("epic attachment not deleted: %#v", deletedEpicAttachment.Data)
	}
	var externalEpicUpdate map[string]any
	apiRequest(t, http.MethodPatch, baseURL+"epics/"+strconv.FormatInt(epicID, 10), token, map[string]any{"subject": "external epic edit", "version": epicVersion}, &externalEpicUpdate)
	stdout, stderr, code = runner.run("--json", "epic", "edit", epicTarget, "--subject", "must conflict", "--base-version", strconv.Itoa(epicVersion))
	if code != 6 || strings.TrimSpace(stdout) != "" {
		t.Fatalf("stale epic OCC edit exit=%d stdout=%s stderr=%s", code, stdout, stderr)
	}
	linkedStory := runner.jsonOK("epic", "link", epicTarget, "--story", secondaryStoryTarget)
	if linkedStory.Data["story_project"] != secondaryProjectSlug || linkedStory.Data["story_ref"] != float64(secondaryStoryRef) {
		t.Fatalf("cross-project Epic link=%#v", linkedStory.Data)
	}
	epicStories := runner.jsonOK("epic", "stories", epicTarget, "--fields", "story_project,story_ref,subject")
	if !containsProjectRef(epicStories.Items, secondaryProjectSlug, secondaryStoryRef) {
		t.Fatalf("cross-project Story missing from Epic: %#v", epicStories.Items)
	}
	watchedEpic := runner.jsonOK("epic", "watch", epicTarget)
	if watchedEpic.Data["watching"] != true || watchedEpic.Data["verified"] != true {
		t.Fatalf("epic watch was not verified: %#v", watchedEpic.Data)
	}
	epicHistory := runner.jsonOK("epic", "history", epicTarget, "--type", "activity", "--fields", "id,kind,author,changes")
	if len(epicHistory.Items) == 0 {
		t.Fatal("epic activity history is empty")
	}
	unwatchedEpic := runner.jsonOK("epic", "unwatch", epicTarget)
	if unwatchedEpic.Data["watching"] != false || unwatchedEpic.Data["verified"] != true {
		t.Fatalf("epic unwatch was not verified: %#v", unwatchedEpic.Data)
	}
	votedEpic := runner.jsonOK("epic", "vote", epicTarget)
	if votedEpic.Data["voting"] != true || votedEpic.Data["verified"] != true {
		t.Fatalf("epic vote was not verified: %#v", votedEpic.Data)
	}
	unvotedEpic := runner.jsonOK("epic", "unvote", epicTarget)
	if unvotedEpic.Data["voting"] != false || unvotedEpic.Data["verified"] != true {
		t.Fatalf("epic unvote was not verified: %#v", unvotedEpic.Data)
	}
	unlinkedStory := runner.jsonOK("epic", "unlink", epicTarget, "--story", secondaryStoryTarget)
	if unlinkedStory.Data["linked"] != false {
		t.Fatalf("epic unlink=%#v", unlinkedStory.Data)
	}
	epicStories = runner.jsonOK("epic", "stories", epicTarget, "--fields", "story_project,story_ref")
	if containsProjectRef(epicStories.Items, secondaryProjectSlug, secondaryStoryRef) {
		t.Fatalf("unlinked Story remains in Epic: %#v", epicStories.Items)
	}
	closedEpicStatus := firstClosedEpicStatus(t, baseURL, token, projectID)
	currentEpic := runner.jsonOK("epic", "view", epicTarget, "--fields", "version")
	epicVersion = int(currentEpic.Data["version"].(float64))
	closedEpic := runner.jsonOK("epic", "close", epicTarget, "--status", closedEpicStatus, "--base-version", strconv.Itoa(epicVersion))
	if closedEpic.Data["is_closed"] != true {
		t.Fatalf("epic not closed: %#v", closedEpic.Data)
	}

	storyWithTask := runner.jsonOK("story", "create", "--subject", "Story with open task", "--sprint", milestoneSlug)
	storyWithTaskRef := int(storyWithTask.Data["ref"].(float64))
	storyWithTaskVersion := int(storyWithTask.Data["version"].(float64))
	task := runner.jsonOK("task", "create", "--story", strconv.Itoa(storyWithTaskRef), "--subject", "E2E task")
	taskRef := int(task.Data["ref"].(float64))
	taskVersion := int(task.Data["version"].(float64))
	taskTarget := strconv.Itoa(taskRef)
	if task.Data["story_ref"] != float64(storyWithTaskRef) || task.Data["sprint_slug"] != milestoneSlug {
		t.Fatalf("task did not inherit parent Story/Sprint: %#v", task.Data)
	}
	tasks := runner.jsonOK("task", "list", "--story", strconv.Itoa(storyWithTaskRef), "--fields", "ref,subject,status,story_ref,version")
	if !containsRef(tasks.Items, taskRef) {
		t.Fatalf("created task ref %d missing from parent-filtered list", taskRef)
	}
	runner.jsonOK("task", "view", taskTarget, "--fields", "ref,subject,status,story_ref,version")
	taskDryRun := runner.jsonOK("task", "edit", taskTarget, "--subject", "must not persist", "--dry-run")
	if taskDryRun.Plan["performed"] != false || taskDryRun.Plan["would_write"] != true {
		t.Fatalf("task dry-run plan = %#v", taskDryRun.Plan)
	}
	taskEdited := runner.jsonOK("task", "edit", taskTarget, "--subject", "E2E task updated", "--base-version", strconv.Itoa(taskVersion))
	taskVersion = int(taskEdited.Data["version"].(float64))
	taskAssigned := runner.jsonOK("task", "assign", taskTarget, "--to", username, "--base-version", strconv.Itoa(taskVersion))
	taskVersion = int(taskAssigned.Data["version"].(float64))
	if taskAssigned.Data["assignee"] != username {
		t.Fatalf("task assignee = %#v", taskAssigned.Data)
	}
	taskComment := runner.jsonOK("task", "comment", taskTarget, "--body", "task integration comment", "--base-version", strconv.Itoa(taskVersion))
	taskVersion = int(taskComment.Data["version"].(float64))
	watchedTask := runner.jsonOK("task", "watch", taskTarget)
	if watchedTask.Data["watching"] != true || watchedTask.Data["verified"] != true {
		t.Fatalf("task watch was not verified: %#v", watchedTask.Data)
	}
	taskHistory := runner.jsonOK("task", "history", taskTarget, "--type", "comment", "--fields", "id,kind,author,comment")
	if !containsComment(taskHistory.Items, "task integration comment") {
		t.Fatalf("task comment missing from CLI history: %#v", taskHistory.Items)
	}
	unwatchedTask := runner.jsonOK("task", "unwatch", taskTarget)
	if unwatchedTask.Data["watching"] != false || unwatchedTask.Data["verified"] != true {
		t.Fatalf("task unwatch was not verified: %#v", unwatchedTask.Data)
	}
	votedTask := runner.jsonOK("task", "vote", taskTarget)
	if votedTask.Data["voting"] != true || votedTask.Data["verified"] != true {
		t.Fatalf("task vote was not verified: %#v", votedTask.Data)
	}
	unvotedTask := runner.jsonOK("task", "unvote", taskTarget)
	if unvotedTask.Data["voting"] != false || unvotedTask.Data["verified"] != true {
		t.Fatalf("task unvote was not verified: %#v", unvotedTask.Data)
	}
	stdout, stderr, code = runner.run("story", "close", strconv.Itoa(storyWithTaskRef), "--status", closedStoryStatus, "--base-version", strconv.Itoa(storyWithTaskVersion))
	if code != 0 || !strings.Contains(stderr, "open tasks keep this story active") {
		t.Fatalf("open-task close warning exit=%d stdout=%s stderr=%s", code, stdout, stderr)
	}
	openTaskStory := runner.jsonOK("story", "view", strconv.Itoa(storyWithTaskRef), "--fields", "status,is_closed")
	if openTaskStory.Data["status"] != closedStoryStatus || openTaskStory.Data["is_closed"] != false {
		t.Fatalf("open-task story close semantics = %#v", openTaskStory.Data)
	}
	closedTaskStatus := firstClosedTaskStatus(t, baseURL, token, projectID)
	doneTask := runner.jsonOK("task", "done", taskTarget, "--status", closedTaskStatus, "--base-version", strconv.Itoa(taskVersion))
	if doneTask.Data["is_closed"] != true {
		t.Fatalf("task not done: %#v", doneTask.Data)
	}
	taskVersion = int(doneTask.Data["version"].(float64))
	completedParent := runner.jsonOK("story", "view", strconv.Itoa(storyWithTaskRef), "--fields", "status,is_closed")
	if completedParent.Data["status"] != closedStoryStatus || completedParent.Data["is_closed"] != true {
		t.Fatalf("parent Story did not close after final task: %#v", completedParent.Data)
	}
	openTaskStatus := firstOpenTaskStatus(t, baseURL, token, projectID)
	reopenedTask := runner.jsonOK("task", "reopen", taskTarget, "--status", openTaskStatus, "--base-version", strconv.Itoa(taskVersion))
	if reopenedTask.Data["is_closed"] != false {
		t.Fatalf("task not reopened: %#v", reopenedTask.Data)
	}
	taskVersion = int(reopenedTask.Data["version"].(float64))
	unassignedTask := runner.jsonOK("task", "unassign", taskTarget, "--base-version", strconv.Itoa(taskVersion))
	if value, ok := unassignedTask.Data["assignee"]; ok && value != "" {
		t.Fatalf("task not unassigned: %#v", unassignedTask.Data)
	}
	taskVersion = int(unassignedTask.Data["version"].(float64))
	standaloneSprintTask := runner.jsonOK("task", "move", taskTarget, "--sprint", milestoneSlug, "--base-version", strconv.Itoa(taskVersion))
	if value, ok := standaloneSprintTask.Data["story_ref"]; ok && value != float64(0) {
		t.Fatalf("task still has parent Story: %#v", standaloneSprintTask.Data)
	}
	if standaloneSprintTask.Data["sprint_slug"] != milestoneSlug {
		t.Fatalf("standalone task missing sprint: %#v", standaloneSprintTask.Data)
	}
	taskVersion = int(standaloneSprintTask.Data["version"].(float64))
	parentedTask := runner.jsonOK("task", "move", taskTarget, "--story", strconv.Itoa(storyWithTaskRef), "--base-version", strconv.Itoa(taskVersion))
	if parentedTask.Data["story_ref"] != float64(storyWithTaskRef) || parentedTask.Data["sprint_slug"] != milestoneSlug {
		t.Fatalf("task did not rejoin parent Story: %#v", parentedTask.Data)
	}
	taskVersion = int(parentedTask.Data["version"].(float64))
	backlogTask := runner.jsonOK("task", "move", taskTarget, "--sprint", "backlog", "--base-version", strconv.Itoa(taskVersion))
	if value, ok := backlogTask.Data["story_ref"]; ok && value != float64(0) {
		t.Fatalf("backlog task still has parent Story: %#v", backlogTask.Data)
	}
	if value, ok := backlogTask.Data["sprint_slug"]; ok && value != "" {
		t.Fatalf("backlog task still has sprint: %#v", backlogTask.Data)
	}

	batchPath := filepath.Join(runner.dir, "batch-subjects.txt")
	if err := os.WriteFile(batchPath, []byte("Batch first\nBatch second\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	batchDryRun := runner.jsonOK("batch", "create", "story", batchPath, "--dry-run")
	if batchDryRun.Plan["performed"] != false || batchDryRun.Plan["would_write"] != true {
		t.Fatalf("batch dry-run plan=%#v", batchDryRun.Plan)
	}
	var batchStoryIDs []string
	var batchTaskOrders []string
	for _, batch := range []struct {
		resource string
		args     []string
	}{
		{resource: "epic", args: []string{"batch", "create", "epic", batchPath, "--yes"}},
		{resource: "story", args: []string{"batch", "create", "story", batchPath, "--yes"}},
		{resource: "issue", args: []string{"batch", "create", "issue", batchPath, "--sprint", milestoneSlug, "--yes"}},
		{resource: "task", args: []string{"batch", "create", "task", batchPath, "--story", strconv.Itoa(storyWithTaskRef), "--yes"}},
	} {
		created := runner.jsonOK(batch.args...)
		if len(created.Items) != 2 || created.Page["created"] != float64(2) || created.Page["verified"] != true {
			t.Fatalf("%s batch result=%#v page=%#v", batch.resource, created.Items, created.Page)
		}
		for _, item := range created.Items {
			if item["resource"] != batch.resource || item["project"] != projectSlug {
				t.Fatalf("%s batch item=%#v", batch.resource, item)
			}
			id := int64(item["id"].(float64))
			if batch.resource == "story" {
				batchStoryIDs = append(batchStoryIDs, strconv.FormatInt(id, 10))
			}
			if batch.resource == "task" {
				batchTaskOrders = append(batchTaskOrders, fmt.Sprintf("%d=%d", id, len(batchTaskOrders)+1))
			}
		}
	}
	moveArgs := []string{"batch", "move", "story", "--sprint", milestoneSlug, "--yes"}
	for _, id := range batchStoryIDs {
		moveArgs = append(moveArgs, "--id", id)
	}
	runner.jsonOK(moveArgs...)
	reorderStoryArgs := []string{"batch", "reorder", "story", "--view", "backlog", "--sprint", milestoneSlug, "--yes"}
	for _, id := range batchStoryIDs {
		reorderStoryArgs = append(reorderStoryArgs, "--id", id)
	}
	runner.jsonOK(reorderStoryArgs...)
	reorderTaskArgs := []string{"batch", "reorder", "task", "--view", "taskboard", "--sprint", milestoneSlug, "--yes"}
	for _, order := range batchTaskOrders {
		reorderTaskArgs = append(reorderTaskArgs, "--order", order)
	}
	runner.jsonOK(reorderTaskArgs...)

	for _, metadata := range []struct {
		kind       string
		createArgs []string
		editArgs   []string
	}{
		{kind: "epic-status", createArgs: []string{"--color", "#123456"}, editArgs: []string{"--color", "#654321"}},
		{kind: "story-status", createArgs: []string{"--color", "#123456", "--wip-limit", "3"}, editArgs: []string{"--color", "#654321", "--wip-limit", "4"}},
		{kind: "task-status", createArgs: []string{"--color", "#123456"}, editArgs: []string{"--color", "#654321"}},
		{kind: "issue-status", createArgs: []string{"--color", "#123456"}, editArgs: []string{"--color", "#654321"}},
		{kind: "points", createArgs: []string{"--value", "13"}, editArgs: []string{"--value", "21"}},
		{kind: "priority", createArgs: []string{"--color", "#123456"}, editArgs: []string{"--color", "#654321"}},
		{kind: "severity", createArgs: []string{"--color", "#123456"}, editArgs: []string{"--color", "#654321"}},
		{kind: "issue-type", createArgs: []string{"--color", "#123456"}, editArgs: []string{"--color", "#654321"}},
	} {
		before := runner.jsonOK("metadata", "list", metadata.kind, "--fields", "id,name")
		if len(before.Items) == 0 {
			t.Fatalf("%s has no replacement metadata", metadata.kind)
		}
		replacementID := int64(before.Items[0]["id"].(float64))
		name := "E2E " + metadata.kind
		createArgs := append([]string{"metadata", "create", metadata.kind, "--name", name}, metadata.createArgs...)
		created := runner.jsonOK(createArgs...)
		createdID := int64(created.Data["id"].(float64))
		updatedName := name + " Updated"
		editArgs := append([]string{"metadata", "edit", metadata.kind, strconv.FormatInt(createdID, 10), "--name", updatedName}, metadata.editArgs...)
		updated := runner.jsonOK(editArgs...)
		if updated.Data["name"] != updatedName {
			t.Fatalf("%s metadata edit=%#v", metadata.kind, updated.Data)
		}
		deleted := runner.jsonOK("metadata", "delete", metadata.kind, strconv.FormatInt(createdID, 10), "--move-to", strconv.FormatInt(replacementID, 10), "--yes")
		if deleted.Data["deleted"] != true || deleted.Data["verified"] != true {
			t.Fatalf("%s metadata delete=%#v", metadata.kind, deleted.Data)
		}
	}

	dueDate := runner.jsonOK("due-date", "create", "story", "--name", "E2E due date", "--days", "5", "--color", "#123456")
	dueDateID := int64(dueDate.Data["id"].(float64))
	dueDateUpdated := runner.jsonOK("due-date", "edit", "story", strconv.FormatInt(dueDateID, 10), "--days", "7")
	if dueDateUpdated.Data["days_to_due"] != float64(7) {
		t.Fatalf("due-date edit=%#v", dueDateUpdated.Data)
	}
	dueDateDeleted := runner.jsonOK("due-date", "delete", "story", strconv.FormatInt(dueDateID, 10), "--yes")
	if dueDateDeleted.Data["verified"] != true {
		t.Fatalf("due-date delete=%#v", dueDateDeleted.Data)
	}

	tag := runner.jsonOK("tag", "create", "e2e-source", "--color", "#123456")
	if tag.Data["name"] != "e2e-source" {
		t.Fatalf("tag create=%#v", tag.Data)
	}
	runner.jsonOK("tag", "create", "e2e-target", "--color", "#654321")
	runner.jsonOK("tag", "mix", "e2e-target", "--from", "e2e-source")
	tags := runner.jsonOK("tag", "list")
	if len(tags.Items) == 0 {
		t.Fatalf("tag list=%#v", tags.Items)
	}
	runner.jsonOK("tag", "delete", "e2e-target", "--yes")

	storage := runner.jsonOK("storage", "set", "e2e-settings", "--value", `{"compact":true}`)
	if storage.Data["key"] != "e2e-settings" {
		t.Fatalf("storage set=%#v", storage.Data)
	}
	storage = runner.jsonOK("storage", "get", "e2e-settings")
	if storage.Data["value"].(map[string]any)["compact"] != true {
		t.Fatalf("storage get=%#v", storage.Data)
	}
	runner.jsonOK("storage", "delete", "e2e-settings", "--yes")

	policies := runner.jsonOK("notification", "policy", "list")
	var projectPolicyID int64
	for _, policy := range policies.Items {
		if int64(policy["project"].(float64)) == projectID {
			projectPolicyID = int64(policy["id"].(float64))
			break
		}
	}
	if projectPolicyID == 0 {
		t.Fatalf("project notification policy missing: %#v", policies.Items)
	}
	policy := runner.jsonOK("notification", "policy", "edit", strconv.FormatInt(projectPolicyID, 10), "--email", "involved", "--live", "all", "--web=true")
	if policy.Data["notify_level"] != float64(1) || policy.Data["live_notify_level"] != float64(2) {
		t.Fatalf("notification policy edit=%#v", policy.Data)
	}
	runner.jsonOK("notification", "web", "list", "--unread")
	runner.jsonOK("notification", "web", "read", "--all")
	for _, args := range [][]string{{"--json", "application", "list"}, {"--json", "application", "tokens"}} {
		stdout, stderr, code = runner.run(args...)
		if code != 0 && (code != 5 || !strings.Contains(stderr, "not_found")) {
			t.Fatalf("application capability exit=%d stdout=%s stderr=%s", code, stdout, stderr)
		}
	}

	liked := runner.jsonOK("project", "like")
	if liked.Data["liked"] != true {
		t.Fatalf("project like=%#v", liked.Data)
	}
	fans := runner.jsonOK("project", "fans")
	if !containsUsername(fans.Items, username) {
		t.Fatalf("project fans=%#v", fans.Items)
	}
	runner.jsonOK("project", "unlike")

	csvPath := filepath.Join(runner.dir, "issues.csv")
	csvExport := runner.jsonOK("csv", "export", "issue", "--output", csvPath)
	if csvExport.Data["token_revoked"] != true || csvExport.Data["verified"] != true {
		t.Fatalf("csv export=%#v", csvExport.Data)
	}
	if stat, err := os.Stat(csvPath); err != nil || stat.Mode().Perm() != 0o600 {
		t.Fatalf("csv file stat=%v err=%v", stat, err)
	}

	lane := runner.jsonOK("swimlane", "create", "--name", "E2E lane", "--order", "5")
	laneID := int64(lane.Data["id"].(float64))
	lanes := runner.jsonOK("swimlane", "list")
	if !containsID(lanes.Items, laneID) {
		t.Fatalf("swimlane list=%#v", lanes.Items)
	}
	storyStatuses := runner.jsonOK("metadata", "list", "story-status")
	statusName := storyStatuses.Items[0]["name"].(string)
	wip := runner.jsonOK("swimlane", "wip", strconv.FormatInt(laneID, 10), statusName, "--limit", "3")
	if wip.Data["wip_limit"] != float64(3) {
		t.Fatalf("swimlane WIP=%#v", wip.Data)
	}
	laneDeleted := runner.jsonOK("swimlane", "delete", strconv.FormatInt(laneID, 10), "--yes")
	if laneDeleted.Data["verified"] != true {
		t.Fatalf("swimlane delete=%#v", laneDeleted.Data)
	}

	deleteSprint := runner.jsonOK("sprint", "create", "--name", "Delete E2E Sprint", "--start", "2026-09-10", "--finish", "2026-09-12")
	deleteSprintResult := runner.jsonOK("sprint", "delete", deleteSprint.Data["slug"].(string), "--yes")
	if deleteSprintResult.Data["verified"] != true {
		t.Fatalf("sprint delete=%#v", deleteSprintResult.Data)
	}

	for _, deletion := range []struct {
		resource string
		ref      int
	}{
		{resource: "issue", ref: int(runner.jsonOK("issue", "create", "--subject", "Delete E2E issue").Data["ref"].(float64))},
		{resource: "story", ref: int(runner.jsonOK("story", "create", "--subject", "Delete E2E story").Data["ref"].(float64))},
		{resource: "task", ref: int(runner.jsonOK("task", "create", "--subject", "Delete E2E task").Data["ref"].(float64))},
		{resource: "epic", ref: int(runner.jsonOK("epic", "create", "--subject", "Delete E2E epic").Data["ref"].(float64))},
	} {
		deleted := runner.jsonOK(deletion.resource, "delete", strconv.Itoa(deletion.ref), "--yes")
		if deleted.Data["deleted"] != true || deleted.Data["verified"] != true {
			t.Fatalf("%s deletion=%#v", deletion.resource, deleted.Data)
		}
	}

	editedSprint := runner.jsonOK("sprint", "edit", milestoneSlug, "--finish", "2026-09-08")
	if editedSprint.Data["finish"] != "2026-09-08" {
		t.Fatalf("sprint finish was not updated: %#v", editedSprint.Data)
	}
	closedSprint := runner.jsonOK("sprint", "close", milestoneSlug)
	if closedSprint.Data["closed"] != true {
		t.Fatalf("sprint not closed: %#v", closedSprint.Data)
	}
	closedSprints := runner.jsonOK("sprint", "list", "--state", "closed", "--fields", "slug,closed")
	if !containsSlug(closedSprints.Items, milestoneSlug) {
		t.Fatalf("closed sprint %q missing from closed list", milestoneSlug)
	}
	reopenedSprint := runner.jsonOK("sprint", "reopen", milestoneSlug)
	if reopenedSprint.Data["closed"] != false {
		t.Fatalf("sprint not reopened: %#v", reopenedSprint.Data)
	}

	wikiSlug := "e2e-guide"
	wiki := runner.jsonOK("wiki", "create", "--slug", wikiSlug, "--body", "# E2E guide\nInitial content")
	wikiID := int64(wiki.Data["id"].(float64))
	wikiVersion := int(wiki.Data["version"].(float64))
	wikiPages := runner.jsonOK("wiki", "list", "--fields", "slug,version,editions")
	if !containsSlug(wikiPages.Items, wikiSlug) {
		t.Fatalf("created wiki page %q missing from list", wikiSlug)
	}
	wikiView := runner.jsonOK("wiki", "view", wikiSlug, "--fields", "slug,content,version,is_watcher")
	if wikiView.Data["content"] != "# E2E guide\nInitial content" {
		t.Fatalf("wiki content=%#v", wikiView.Data)
	}
	wikiDryRun := runner.jsonOK("wiki", "edit", wikiSlug, "--body", "must not persist", "--dry-run")
	if wikiDryRun.Plan["performed"] != false || wikiDryRun.Plan["would_write"] != true {
		t.Fatalf("wiki dry-run plan=%#v", wikiDryRun.Plan)
	}
	wikiEdited := runner.jsonOKWithInput("# E2E guide\nUpdated from stdin", "wiki", "edit", wikiSlug, "--body-file", "-", "--base-version", strconv.Itoa(wikiVersion))
	wikiVersion = int(wikiEdited.Data["version"].(float64))
	if wikiEdited.Data["content"] != "# E2E guide\nUpdated from stdin" {
		t.Fatalf("wiki stdin update=%#v", wikiEdited.Data)
	}
	wikiLink := runner.jsonOK("wiki-link", "create", "--title", "E2E Link Page")
	wikiLinkID := int64(wikiLink.Data["id"].(float64))
	wikiLinkHref := wikiLink.Data["href"].(string)
	wikiLinks := runner.jsonOK("wiki-link", "list", "--fields", "id,title,href,order")
	if !containsID(wikiLinks.Items, wikiLinkID) {
		t.Fatalf("created Wiki link missing: %#v", wikiLinks.Items)
	}
	editedWikiLink := runner.jsonOK("wiki-link", "edit", strconv.FormatInt(wikiLinkID, 10), "--title", "E2E Link Updated")
	if editedWikiLink.Data["title"] != "E2E Link Updated" {
		t.Fatalf("Wiki link edit=%#v", editedWikiLink.Data)
	}
	deletedWikiLink := runner.jsonOK("wiki-link", "delete", strconv.FormatInt(wikiLinkID, 10), "--yes")
	if deletedWikiLink.Data["deleted"] != true || deletedWikiLink.Data["page_deleted"] != false {
		t.Fatalf("Wiki link delete=%#v", deletedWikiLink.Data)
	}
	retainedWikiPage := runner.jsonOK("wiki", "view", wikiLinkHref, "--fields", "slug,version")
	if retainedWikiPage.Data["slug"] != wikiLinkHref {
		t.Fatalf("Wiki link page was not retained: %#v", retainedWikiPage.Data)
	}
	runner.jsonOK("wiki", "delete", wikiLinkHref, "--yes")
	wikiAttachment := runner.jsonOK("attachment", "add", "wiki", wikiSlug, attachmentPath, "--description", "Wiki evidence")
	wikiAttachmentID := int64(wikiAttachment.Data["id"].(float64))
	wikiAttachments := runner.jsonOK("attachment", "list", "wiki", wikiSlug, "--fields", "id,name,description")
	if !containsID(wikiAttachments.Items, wikiAttachmentID) {
		t.Fatalf("wiki attachment %d missing from list", wikiAttachmentID)
	}
	deletedWikiAttachment := runner.jsonOK("attachment", "delete", "wiki", strconv.FormatInt(wikiAttachmentID, 10), "--yes")
	if deletedWikiAttachment.Data["deleted"] != true {
		t.Fatalf("wiki attachment not deleted: %#v", deletedWikiAttachment.Data)
	}
	var externalWikiUpdate map[string]any
	apiRequest(t, http.MethodPatch, baseURL+"wiki/"+strconv.FormatInt(wikiID, 10), token, map[string]any{"content": "external wiki edit", "version": wikiVersion}, &externalWikiUpdate)
	stdout, stderr, code = runner.run("--json", "wiki", "edit", wikiSlug, "--body", "must conflict", "--base-version", strconv.Itoa(wikiVersion))
	if code != 6 || strings.TrimSpace(stdout) != "" {
		t.Fatalf("stale wiki OCC edit exit=%d stdout=%s stderr=%s", code, stdout, stderr)
	}
	watchedWiki := runner.jsonOK("wiki", "watch", wikiSlug)
	if watchedWiki.Data["watching"] != true || watchedWiki.Data["verified"] != true {
		t.Fatalf("wiki watch was not verified: %#v", watchedWiki.Data)
	}
	wikiHistory := runner.jsonOK("wiki", "history", wikiSlug, "--type", "activity", "--fields", "id,kind,author,changes")
	if len(wikiHistory.Items) == 0 {
		t.Fatalf("wiki activity history is empty")
	}
	unwatchedWiki := runner.jsonOK("wiki", "unwatch", wikiSlug)
	if unwatchedWiki.Data["watching"] != false || unwatchedWiki.Data["verified"] != true {
		t.Fatalf("wiki unwatch was not verified: %#v", unwatchedWiki.Data)
	}
	stdout, stderr, code = runner.run("--json", "--no-input", "wiki", "delete", wikiSlug)
	if code != 10 || strings.TrimSpace(stdout) != "" {
		t.Fatalf("unconfirmed wiki delete exit=%d stdout=%s stderr=%s", code, stdout, stderr)
	}
	deletedWiki := runner.jsonOK("wiki", "delete", wikiSlug, "--yes")
	if deletedWiki.Data["deleted"] != true {
		t.Fatalf("wiki page not deleted: %#v", deletedWiki.Data)
	}
	wikiPages = runner.jsonOK("wiki", "list", "--fields", "slug")
	if containsSlug(wikiPages.Items, wikiSlug) {
		t.Fatalf("deleted wiki page %q remains in list", wikiSlug)
	}

	searchResults := runner.jsonOK("search", "external", "--fields", "kind,ref,subject")
	if !containsKind(searchResults.Items, "issue") || !containsKind(searchResults.Items, "story") {
		t.Fatalf("search did not return issue and story results: %#v", searchResults.Items)
	}
	taskSearch := runner.jsonOK("search", "task", "--type", "task", "--fields", "kind,ref,subject")
	if len(taskSearch.Items) == 0 || !containsKind(taskSearch.Items, "task") {
		t.Fatalf("task search returned no tasks: %#v", taskSearch.Items)
	}

	timeline := runner.jsonOK("timeline", "--limit", "100", "--fields", "id,resource,action,ref,slug,subject,username,created")
	if len(timeline.Items) == 0 || !containsTimelineResource(timeline.Items, "issue") || !containsTimelineResource(timeline.Items, "story") {
		t.Fatalf("project timeline missing expected resources: %#v", timeline.Items)
	}
	projectStats := runner.jsonOK("stats", "project", projectSlug, "--fields", "project,name,defined_points,assigned_points,closed_points,speed,milestones")
	if projectStats.Data["project"] != projectSlug || projectStats.Data["name"] != project["name"] {
		t.Fatalf("project stats=%#v", projectStats.Data)
	}
	issueStats := runner.jsonOK("stats", "issues", projectSlug, "--fields", "project,total_issues,opened_issues,closed_issues,last_four_weeks_days")
	if issueStats.Data["total_issues"].(float64) < 1 || issueStats.Data["project"] != projectSlug {
		t.Fatalf("issue stats=%#v", issueStats.Data)
	}
	memberStats := runner.jsonOK("stats", "members", projectSlug, "--fields", "id,username,created_bugs,closed_bugs,closed_tasks,wiki_changes")
	if !containsUsername(memberStats.Items, username) {
		t.Fatalf("member stats missing owner %q: %#v", username, memberStats.Items)
	}
	sprintStats := runner.jsonOK("stats", "sprint", milestoneSlug, "--fields", "project,slug,name,total_userstories,completed_userstories,total_tasks,completed_tasks,days")
	if sprintStats.Data["project"] != projectSlug || sprintStats.Data["slug"] != milestoneSlug {
		t.Fatalf("sprint stats=%#v", sprintStats.Data)
	}
	discoverStats := runner.jsonOK("stats", "discover", "--fields", "projects")
	if discoverStats.Data["projects"] == nil {
		t.Fatalf("discover stats=%#v", discoverStats.Data)
	}

	sourceContent := projectContentCounts(t, baseURL, token, projectID)
	exportResult := runner.jsonOK("project", "export", projectSlug, "--format", "gzip")
	if exportResult.Data["status"] != "accepted" || exportResult.Data["verified"] != false {
		t.Fatalf("async project export=%#v", exportResult.Data)
	}
	exportID, _ := exportResult.Data["export_id"].(string)
	if exportID == "" {
		t.Fatalf("project export missing export_id: %#v", exportResult.Data)
	}
	dumpPath := filepath.Join(runner.dir, "project-export.json.gz")
	dumpURL := fmt.Sprintf("%smedia/exports/%d/%s-%s.json.gz", host, projectID, projectSlug, exportID)
	waitForProjectDump(t, dumpURL, dumpPath)
	importRunner := runner
	importRunner.env = replaceEnv(runner.env, "TAIGA_TOKEN", memberToken)
	importResult := importRunner.jsonOK("project", "import", dumpPath, "--yes")
	if importResult.Data["status"] != "accepted" || importResult.Data["verified"] != false {
		t.Fatalf("async project import=%#v", importResult.Data)
	}
	if importID, _ := importResult.Data["import_id"].(string); importID == "" {
		t.Fatalf("project import missing import_id: %#v", importResult.Data)
	}
	importedProjectID := waitForImportedProject(t, baseURL, memberToken, project["name"].(string), projectID, secondaryProjectID)
	waitForImportedContent(t, baseURL, memberToken, importedProjectID, sourceContent)
	deleteProject(t, baseURL, memberToken, importedProjectID)

	// The pre-rename variable has to keep authenticating a real binary, not just
	// satisfy a unit test, because CI jobs written before the rename still export it.
	legacyRunner := runner
	legacyRunner.env = replaceEnv(runner.env, "TAIGA_TOKEN", "")
	legacyRunner.env = replaceEnv(legacyRunner.env, "TAIGA_TOKEN", token)
	legacyStatus := legacyRunner.jsonOK("auth", "status", "--fields", "authenticated")
	if legacyStatus.Data["authenticated"] != true {
		t.Fatalf("TAIGA_TOKEN no longer authenticates: %#v", legacyStatus.Data)
	}

	invalidRunner := runner
	invalidRunner.env = replaceEnv(runner.env, "TAIGA_TOKEN", "token-that-must-never-appear")
	stdout, stderr, code = invalidRunner.run("--verbose", "auth", "status")
	if code != 3 {
		t.Fatalf("invalid token exit=%d stdout=%s stderr=%s", code, stdout, stderr)
	}
	if strings.Contains(stdout+stderr, "token-that-must-never-appear") {
		t.Fatalf("credential leaked: stdout=%s stderr=%s", stdout, stderr)
	}
}

type cliRunner struct {
	t      *testing.T
	binary string
	dir    string
	env    []string
}

func (r cliRunner) run(args ...string) (string, string, int) {
	return r.runWithInput("", args...)
}

func (r cliRunner) runWithInput(input string, args ...string) (string, string, int) {
	r.t.Helper()
	command := exec.Command(r.binary, args...)
	command.Dir = r.dir
	command.Env = append(os.Environ(), r.env...)
	if input != "" {
		command.Stdin = strings.NewReader(input)
	}
	var stdout, stderr bytes.Buffer
	command.Stdout, command.Stderr = &stdout, &stderr
	err := command.Run()
	if err == nil {
		return stdout.String(), stderr.String(), 0
	}
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) {
		r.t.Fatalf("run %v: %v", args, err)
	}
	return stdout.String(), stderr.String(), exitErr.ExitCode()
}

func (r cliRunner) jsonOKWithInput(input string, args ...string) envelope {
	r.t.Helper()
	args = append([]string{"--json"}, args...)
	stdout, stderr, code := r.runWithInput(input, args...)
	if code != 0 {
		r.t.Fatalf("taiga %v exit=%d stderr=%s", args, code, stderr)
	}
	if strings.TrimSpace(stderr) != "" {
		r.t.Fatalf("taiga %v wrote stderr on success: %s", args, stderr)
	}
	var result envelope
	if err := json.Unmarshal([]byte(stdout), &result); err != nil {
		r.t.Fatalf("taiga %v returned invalid JSON: %v: %s", args, err, stdout)
	}
	return result
}

func (r cliRunner) jsonOK(args ...string) envelope {
	r.t.Helper()
	args = append([]string{"--json"}, args...)
	stdout, stderr, code := r.run(args...)
	if code != 0 {
		r.t.Fatalf("taiga %v exit=%d stderr=%s", args, code, stderr)
	}
	if strings.TrimSpace(stderr) != "" {
		r.t.Fatalf("taiga %v wrote stderr on success: %s", args, stderr)
	}
	var result envelope
	if err := json.Unmarshal([]byte(stdout), &result); err != nil {
		r.t.Fatalf("taiga %v returned invalid JSON: %v: %s", args, err, stdout)
	}
	return result
}

func requiredEnv(t *testing.T, name string) string {
	t.Helper()
	value := os.Getenv(name)
	if value == "" {
		t.Fatalf("%s is required", name)
	}
	return value
}

func register(t *testing.T, baseURL, username, password string) string {
	t.Helper()
	body := map[string]any{"accepted_terms": true, "email": username + "@localhost.invalid", "full_name": "Taiga CLI E2E", "password": password, "type": "public", "username": username}
	var response map[string]any
	apiRequest(t, http.MethodPost, baseURL+"auth/register", "", body, &response)
	return response["auth_token"].(string)
}

func verifyEmail(t *testing.T, username string) {
	t.Helper()
	code := "from taiga.users.models import User; User.objects.filter(username='" + username + "').update(verified_email=True)"
	command := exec.Command("docker", "compose", "--project-name", composeProject(), "--file", "docker-compose.yml", "exec", "-T", "taiga-back", "python", "manage.py", "shell", "-c", code)
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("verify E2E email: %v: %s", err, output)
	}
}

func createProject(t *testing.T, baseURL, token string) map[string]any {
	t.Helper()
	var templates []map[string]any
	apiRequest(t, http.MethodGet, baseURL+"project-templates", token, nil, &templates)
	body := map[string]any{"name": "Taiga CLI E2E", "description": "temporary integration project", "creation_template": templates[0]["id"], "is_private": true}
	var project map[string]any
	apiRequest(t, http.MethodPost, baseURL+"projects", token, body, &project)
	return project
}

func waitForProjectDump(t *testing.T, rawURL, path string) {
	t.Helper()
	deadline := time.Now().Add(90 * time.Second)
	for time.Now().Before(deadline) {
		response, err := http.Get(rawURL)
		if err == nil && response.StatusCode == http.StatusOK {
			data, readErr := io.ReadAll(response.Body)
			bodyCloseErr := response.Body.Close()
			if readErr == nil && bodyCloseErr == nil && validGzip(data) {
				if writeErr := os.WriteFile(path, data, 0o600); writeErr != nil {
					t.Fatal(writeErr)
				}
				return
			}
		}
		if response != nil {
			_ = response.Body.Close()
		}
		time.Sleep(500 * time.Millisecond)
	}
	t.Fatalf("project dump was not ready before timeout: %s", rawURL)
}

func validGzip(data []byte) bool {
	reader, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return false
	}
	_, copyErr := io.Copy(io.Discard, reader)
	closeErr := reader.Close()
	return copyErr == nil && closeErr == nil
}

func readDiagnosticZip(t *testing.T, path string) []byte {
	t.Helper()
	reader, err := zip.OpenReader(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = reader.Close() }()
	var contents bytes.Buffer
	for _, entry := range reader.File {
		part, err := entry.Open()
		if err != nil {
			t.Fatal(err)
		}
		_, copyErr := io.Copy(&contents, part)
		closeErr := part.Close()
		if copyErr != nil || closeErr != nil {
			t.Fatalf("read diagnostic entry %s: copy=%v close=%v", entry.Name, copyErr, closeErr)
		}
	}
	return contents.Bytes()
}

// projectContent counts the work items a project holds. Taiga's asynchronous
// importer creates the project row first and fills these in afterwards, so the
// counts double as a completion signal and as evidence that the dump actually
// carried the content.
type projectContent struct {
	Stories int
	Tasks   int
	Issues  int
}

func projectContentCounts(t *testing.T, baseURL, token string, projectID int64) projectContent {
	t.Helper()
	count := func(resource string) int {
		var items []map[string]any
		apiRequest(t, http.MethodGet, fmt.Sprintf("%s%s?project=%d&page_size=1000", baseURL, resource, projectID), token, nil, &items)
		return len(items)
	}
	return projectContent{Stories: count("userstories"), Tasks: count("tasks"), Issues: count("issues")}
}

// waitForImportedContent blocks until an imported project holds what the source
// project held. Deleting before the importer finishes makes Taiga fail its
// cascade with a 500, which is what used to make this test flaky.
func waitForImportedContent(t *testing.T, baseURL, token string, projectID int64, want projectContent) {
	t.Helper()
	deadline := time.Now().Add(90 * time.Second)
	var got projectContent
	for time.Now().Before(deadline) {
		got = projectContentCounts(t, baseURL, token, projectID)
		if got == want {
			return
		}
		time.Sleep(500 * time.Millisecond)
	}
	t.Fatalf("imported project %d holds %+v, want %+v", projectID, got, want)
}

// deleteProject removes a project, tolerating a 404 from an earlier attempt and
// retrying while Taiga reports a server error, because a cascade delete can
// still collide with the tail of an asynchronous import.
func deleteProject(t *testing.T, baseURL, token string, projectID int64) {
	t.Helper()
	url := baseURL + "projects/" + strconv.FormatInt(projectID, 10)
	deadline := time.Now().Add(60 * time.Second)
	for {
		status, body := apiStatus(t, http.MethodDelete, url, token)
		if status == http.StatusNoContent || status == http.StatusOK || status == http.StatusNotFound {
			return
		}
		if status < 500 || !time.Now().Before(deadline) {
			t.Fatalf("DELETE %s returned %d: %s", url, status, body)
		}
		time.Sleep(time.Second)
	}
}

func waitForImportedProject(t *testing.T, baseURL, token, name string, excludedIDs ...int64) int64 {
	t.Helper()
	excluded := map[int64]bool{}
	for _, id := range excludedIDs {
		excluded[id] = true
	}
	deadline := time.Now().Add(90 * time.Second)
	for time.Now().Before(deadline) {
		var projects []map[string]any
		apiRequest(t, http.MethodGet, baseURL+"projects?page_size=1000", token, nil, &projects)
		for _, project := range projects {
			id := int64(project["id"].(float64))
			if project["name"] == name && !excluded[id] {
				return id
			}
		}
		time.Sleep(500 * time.Millisecond)
	}
	t.Fatalf("imported project %q was not visible before timeout", name)
	return 0
}

func firstProjectTemplate(t *testing.T, baseURL, token string) string {
	t.Helper()
	var templates []map[string]any
	apiRequest(t, http.MethodGet, baseURL+"project-templates", token, nil, &templates)
	if len(templates) == 0 {
		t.Fatal("Taiga has no project template")
	}
	if slug, _ := templates[0]["slug"].(string); slug != "" {
		return slug
	}
	return strconv.FormatInt(int64(templates[0]["id"].(float64)), 10)
}

func firstClosedStatus(t *testing.T, baseURL, token string, projectID int64) string {
	t.Helper()
	var statuses []map[string]any
	apiRequest(t, http.MethodGet, baseURL+"issue-statuses?project="+strconv.FormatInt(projectID, 10), token, nil, &statuses)
	for _, status := range statuses {
		if status["is_closed"] == true {
			return status["name"].(string)
		}
	}
	t.Fatal("project has no closed issue status")
	return ""
}

func firstClosedStoryStatus(t *testing.T, baseURL, token string, projectID int64) string {
	t.Helper()
	var statuses []map[string]any
	apiRequest(t, http.MethodGet, baseURL+"userstory-statuses?project="+strconv.FormatInt(projectID, 10), token, nil, &statuses)
	for _, status := range statuses {
		if status["is_closed"] == true {
			return status["name"].(string)
		}
	}
	t.Fatal("project has no closed user story status")
	return ""
}

func firstClosedEpicStatus(t *testing.T, baseURL, token string, projectID int64) string {
	t.Helper()
	var statuses []map[string]any
	apiRequest(t, http.MethodGet, baseURL+"epic-statuses?project="+strconv.FormatInt(projectID, 10), token, nil, &statuses)
	for _, status := range statuses {
		if status["is_closed"] == true {
			return status["name"].(string)
		}
	}
	t.Fatal("project has no closed epic status")
	return ""
}

func firstClosedTaskStatus(t *testing.T, baseURL, token string, projectID int64) string {
	t.Helper()
	var statuses []map[string]any
	apiRequest(t, http.MethodGet, baseURL+"task-statuses?project="+strconv.FormatInt(projectID, 10), token, nil, &statuses)
	for _, status := range statuses {
		if status["is_closed"] == true {
			return status["name"].(string)
		}
	}
	t.Fatal("project has no closed task status")
	return ""
}

func firstOpenTaskStatus(t *testing.T, baseURL, token string, projectID int64) string {
	t.Helper()
	var statuses []map[string]any
	apiRequest(t, http.MethodGet, baseURL+"task-statuses?project="+strconv.FormatInt(projectID, 10), token, nil, &statuses)
	for _, status := range statuses {
		if status["is_closed"] == false {
			return status["name"].(string)
		}
	}
	t.Fatal("project has no open task status")
	return ""
}

// apiStatus performs a request and returns its status, leaving the decision
// about whether that status is a failure to the caller.
func apiStatus(t *testing.T, method, url, token string) (int, []byte) {
	t.Helper()
	request, err := http.NewRequest(method, url, nil)
	if err != nil {
		t.Fatal(err)
	}
	if token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = response.Body.Close() }()
	data, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	return response.StatusCode, data
}

func apiRequest(t *testing.T, method, url, token string, body, output any) {
	t.Helper()
	var reader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		reader = bytes.NewReader(data)
	}
	request, err := http.NewRequest(method, url, reader)
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Content-Type", "application/json")
	if token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	data, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		t.Fatalf("%s %s returned %d: %s", method, url, response.StatusCode, data)
	}
	if output != nil && len(data) > 0 {
		if err := json.Unmarshal(data, output); err != nil {
			t.Fatalf("decode %s %s: %v: %s", method, url, err, data)
		}
	}
}

func containsRef(items []map[string]any, ref int) bool {
	for _, item := range items {
		if int(item["ref"].(float64)) == ref {
			return true
		}
	}
	return false
}

func containsProjectRef(items []map[string]any, project string, ref int) bool {
	for _, item := range items {
		if item["story_project"] == project && int(item["story_ref"].(float64)) == ref {
			return true
		}
	}
	return false
}

func containsMembership(items []map[string]any, id int64, email string) bool {
	for _, item := range items {
		if int64(item["id"].(float64)) == id && item["user_email"] == email {
			return true
		}
	}
	return false
}

func containsSlug(items []map[string]any, slug string) bool {
	for _, item := range items {
		if item["slug"] == slug {
			return true
		}
	}
	return false
}

func containsKind(items []map[string]any, kind string) bool {
	for _, item := range items {
		if item["kind"] == kind {
			return true
		}
	}
	return false
}

func containsTimelineResource(items []map[string]any, resource string) bool {
	for _, item := range items {
		if item["resource"] == resource {
			return true
		}
	}
	return false
}

func containsUsername(items []map[string]any, username string) bool {
	for _, item := range items {
		if item["username"] == username {
			return true
		}
	}
	return false
}

func containsComment(items []map[string]any, comment string) bool {
	for _, item := range items {
		if item["comment"] == comment {
			return true
		}
	}
	return false
}

func historyIDForComment(t *testing.T, items []map[string]any, comment string) string {
	t.Helper()
	for _, item := range items {
		if item["comment"] == comment {
			id, _ := item["id"].(string)
			if id != "" {
				return id
			}
		}
	}
	t.Fatalf("history comment %q has no ID: %#v", comment, items)
	return ""
}

func containsID(items []map[string]any, id int64) bool {
	for _, item := range items {
		if int64(item["id"].(float64)) == id {
			return true
		}
	}
	return false
}

func replaceEnv(values []string, key, value string) []string {
	prefix := key + "="
	result := make([]string, 0, len(values)+1)
	for _, current := range values {
		if !strings.HasPrefix(current, prefix) {
			result = append(result, current)
		}
	}
	return append(result, prefix+value)
}

// composeProject names the stack this run is talking to, so that a pressure
// run can stand up its own alongside the ordinary suite's.
func composeProject() string {
	if name := os.Getenv("TAIGA_E2E_COMPOSE_PROJECT"); name != "" {
		return name
	}
	return "taiga-cli-e2e"
}
