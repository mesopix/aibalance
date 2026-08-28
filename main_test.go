package main

import (
	"errors"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"aibalance/internal/aibalance"
)

func TestMergeServiceSummary(t *testing.T) {
	testModel := newModel(aibalance.RunOptions{}, aibalance.GUISettings{}, false)
	testModel.summary = map[string]any{
		"generated_at": "2026-08-26 10:00:00 CST",
		"accounts": map[string]any{
			"kimi_coding_plan": map[string]any{"status": "ok"},
		},
	}
	baseSummary := testModel.summary

	if changed := testModel.mergeServiceSummary("", map[string]any{"status": "ok"}, nil); changed {
		t.Error("empty service name should not merge")
	}
	if changed := testModel.mergeServiceSummary("qoder_team_credit", nil, nil); changed {
		t.Error("nil summary should not merge")
	}

	if changed := testModel.mergeServiceSummary("qoder_team_credit",
		map[string]any{"status": "ok"}, "2026-08-26 10:05:00 CST"); !changed {
		t.Fatal("valid result should merge")
	}
	accounts, _ := testModel.summary["accounts"].(map[string]any)
	if _, exists := accounts["kimi_coding_plan"]; !exists {
		t.Error("existing service dropped from merged summary")
	}
	if _, exists := accounts["qoder_team_credit"]; !exists {
		t.Error("new service missing from merged summary")
	}
	if testModel.summary["generated_at"] != "2026-08-26 10:05:00 CST" {
		t.Errorf("generated_at = %v, want the refresh's timestamp",
			testModel.summary["generated_at"])
	}
	// Earlier snapshots of the summary must stay immutable.
	baseAccounts, _ := baseSummary["accounts"].(map[string]any)
	if _, polluted := baseAccounts["qoder_team_credit"]; polluted {
		t.Error("merge mutated the base accounts map in place")
	}
	if len(testModel.views) != 2 {
		t.Errorf("views = %d, want 2", len(testModel.views))
	}
	if testModel.lastRefreshAt["qoder_team_credit"].IsZero() {
		t.Error("merge did not stamp the service refresh time")
	}
}

func TestLaunchServiceRefreshesSkipsInFlight(t *testing.T) {
	testModel := newModel(aibalance.RunOptions{}, aibalance.GUISettings{}, false)
	testModel.inFlight["kimi_coding_plan"] = true
	testModel.notice = "stale hint"

	commands := testModel.launchServiceRefreshes([]string{"kimi_coding_plan", "deepseek_api"})
	if len(commands) != 1 {
		t.Fatalf("got %d commands, want 1 (in-flight service skipped)", len(commands))
	}
	if !testModel.inFlight["deepseek_api"] {
		t.Error("launched service must be marked in-flight before its command runs")
	}
	if testModel.notice != "" {
		t.Error("a fresh refresh must clear the notice")
	}

	if commands := testModel.launchServiceRefreshes([]string{"kimi_coding_plan", "deepseek_api"}); len(commands) != 0 {
		t.Errorf("got %d commands for an all-in-flight input, want 0", len(commands))
	}
}

func TestAutoRefreshTickLaunchesDueServices(t *testing.T) {
	testModel := newModel(aibalance.RunOptions{}, aibalance.GUISettings{AutoRefresh: true}, false)
	testModel.enabledServices = []string{"deepseek_api", "kimi_coding_plan", "qoder_team_credit"}
	firedAt := time.Now()
	testModel.nextDue["deepseek_api"] = firedAt.Add(-time.Minute)
	testModel.nextDue["kimi_coding_plan"] = firedAt.Add(-time.Second)
	// Tiny pending deadline: a regression makes the assertion below invoke the
	// re-armed tick itself, which then returns instead of sleeping.
	testModel.nextDue["qoder_team_credit"] = firedAt.Add(50 * time.Millisecond)

	_, command := testModel.Update(autoRefreshTickMsg{
		generation: testModel.tickGeneration,
		firedAt:    firedAt,
	})
	if command == nil {
		t.Fatal("auto-refresh tick returned no command")
	}
	// Unwrapping the batch reads the command list without running it.
	message := command()
	batch, isBatch := message.(tea.BatchMsg)
	if !isBatch {
		t.Fatalf("tick command produced %T, want tea.BatchMsg", message)
	}
	if len(batch) != 3 {
		t.Errorf("batch holds %d commands, want 3 (two refreshes plus the next tick)", len(batch))
	}
	if !testModel.inFlight["deepseek_api"] || !testModel.inFlight["kimi_coding_plan"] {
		t.Error("due services must be marked in-flight")
	}
	if testModel.inFlight["qoder_team_credit"] {
		t.Error("a service that is not due yet must not be marked in-flight")
	}
}

func TestHandleServiceRefreshDoneMergesAndRearms(t *testing.T) {
	testModel := newModel(aibalance.RunOptions{}, aibalance.GUISettings{AutoRefresh: true}, false)
	testModel.summary = map[string]any{
		"generated_at": "2026-08-26 10:00:00 CST",
		"accounts": map[string]any{
			"kimi_coding_plan": map[string]any{"status": "ok"},
		},
	}
	testModel.inFlight["deepseek_api"] = true
	testModel.inFlight["kimi_coding_plan"] = true
	saveCalls := 0
	testModel.saveSummary = func(map[string]any) error {
		saveCalls++
		return nil
	}

	output := map[string]any{
		"generated_at": "2026-08-26 10:05:00",
		"accounts": map[string]any{
			"deepseek_api": map[string]any{"status": "ok"},
		},
	}
	_, _ = testModel.handleServiceRefreshDone(serviceRefreshDoneMsg{service: "deepseek_api", output: output})

	if testModel.inFlight["deepseek_api"] {
		t.Error("done must clear the service's in-flight mark")
	}
	if testModel.inFlight["kimi_coding_plan"] != true {
		t.Error("done must leave other services' in-flight marks alone")
	}
	accounts, _ := testModel.summary["accounts"].(map[string]any)
	if _, exists := accounts["deepseek_api"]; !exists {
		t.Error("refreshed service missing from merged summary")
	}
	if _, exists := accounts["kimi_coding_plan"]; !exists {
		t.Error("untouched service dropped from merged summary")
	}
	if testModel.source != "live" {
		t.Errorf("source = %q, want live", testModel.source)
	}
	if testModel.err != nil {
		t.Errorf("err = %v, want nil", testModel.err)
	}
	if saveCalls != 1 {
		t.Errorf("saveSummary called %d times, want 1", saveCalls)
	}
	if testModel.lastRefreshAt["deepseek_api"].IsZero() {
		t.Error("done did not stamp the service refresh time")
	}
	if deadline, scheduled := testModel.nextDue["deepseek_api"]; !scheduled || !deadline.After(time.Now()) {
		t.Error("done did not re-arm the service's auto-refresh deadline")
	}
}

func TestHandleServiceRefreshDoneErrorKeepsData(t *testing.T) {
	testModel := newModel(aibalance.RunOptions{}, aibalance.GUISettings{AutoRefresh: true}, false)
	testModel.summary = map[string]any{
		"generated_at": "2026-08-26 10:00:00 CST",
		"accounts": map[string]any{
			"kimi_coding_plan": map[string]any{"status": "ok"},
		},
	}
	testModel.inFlight["kimi_coding_plan"] = true
	saveCalls := 0
	testModel.saveSummary = func(map[string]any) error {
		saveCalls++
		return nil
	}

	_, _ = testModel.handleServiceRefreshDone(serviceRefreshDoneMsg{
		service: "kimi_coding_plan",
		err:     errors.New("boom"),
	})

	if testModel.inFlight["kimi_coding_plan"] {
		t.Error("done must clear the in-flight mark even on error")
	}
	if testModel.err == nil {
		t.Error("error path must surface the error")
	}
	if saveCalls != 0 {
		t.Errorf("saveSummary called %d times on error, want 0", saveCalls)
	}
	accounts, _ := testModel.summary["accounts"].(map[string]any)
	if _, exists := accounts["kimi_coding_plan"]; !exists {
		t.Error("error path must keep the previous card data")
	}
	if deadline, scheduled := testModel.nextDue["kimi_coding_plan"]; !scheduled || !deadline.After(time.Now()) {
		t.Error("error path must still re-arm the deadline (no retry storm)")
	}
}

func TestLoginNeededServicesIncludesViewlessServices(t *testing.T) {
	testModel := newModel(aibalance.RunOptions{}, aibalance.GUISettings{}, false)
	testModel.enabledServices = []string{"chatgpt_codex"}

	// Fresh start, first refresh still in flight: no views, login state
	// unknown — the service must already be a login candidate.
	if got := testModel.loginNeededServices(); len(got) != 1 || got[0] != "chatgpt_codex" {
		t.Errorf("loginNeededServices() = %v, want [chatgpt_codex] with no views", got)
	}

	// A completed refresh that says the session is fine removes the
	// candidate; a NEEDS_LOGIN view keeps it.
	testModel.views = []aibalance.ServiceView{
		{ServiceID: "chatgpt_codex", Name: "ChatGPT Codex", Status: "OK"},
	}
	if got := testModel.loginNeededServices(); len(got) != 0 {
		t.Errorf("loginNeededServices() = %v, want empty for an OK view", got)
	}
	testModel.views[0].Status = "NEEDS_LOGIN"
	if got := testModel.loginNeededServices(); len(got) != 1 || got[0] != "chatgpt_codex" {
		t.Errorf("loginNeededServices() = %v, want [chatgpt_codex] for NEEDS_LOGIN", got)
	}

	// API-key services never get a login page, view or not.
	testModel.enabledServices = []string{"deepseek_api"}
	testModel.views = nil
	if got := testModel.loginNeededServices(); len(got) != 0 {
		t.Errorf("loginNeededServices() = %v, want empty for API-key service", got)
	}
}
