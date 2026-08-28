package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"aibalance/internal/aibalance"
)

// runCLI implements the "aibalance cli" subcommand, the Go replacement for
// tools/ai_balance/ai_balance.py: scrape balances, print JSON / human text /
// progress events. It never launches Chrome; a CDP Chrome must be running.
func runCLI(args []string) {
	flags := flag.NewFlagSet("cli", flag.ExitOnError)
	only := flags.String("only", "all", "comma-separated service IDs or 'all'")
	jsonOutput := flags.Bool("json", false, "Print concise machine-readable JSON.")
	progressJSONL := flags.Bool("progress-jsonl", false, "Print streaming progress events as JSON Lines.")
	debugOutput := flags.Bool("debug", false, "Print raw captured data for troubleshooting.")
	strictExit := flags.Bool("strict-exit", false, "Return a non-zero exit code when any service fails.")
	deepseekTimeout := flags.Int("deepseek-timeout-seconds", 20, "Timeout for the DeepSeek HTTP API in seconds.")
	cdpURL := flags.String("cdp-url", os.Getenv("CHROME_CDP_URL"), "CDP endpoint of the primary automation Chrome.")
	cdpURL2 := flags.String("cdp-url-2", os.Getenv("CHROME_CDP_URL_2"), "CDP endpoint of the second account Chrome (Z.ai #2, BigModel #2).")
	timeoutMS := flags.Int("timeout-ms", 30_000, "Navigation timeout per browser service in milliseconds.")
	waitMS := flags.Int("wait-ms", 3_000, "Extra settle delay per browser service in milliseconds.")
	flags.Parse(args)

	selectedServices, parseErr := aibalance.ParseServices(strings.Split(*only, ","))
	if parseErr != nil {
		fmt.Fprintf(os.Stderr, "%v\n", parseErr)
		os.Exit(2)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	options := aibalance.RunOptions{
		DeepSeekTimeoutSeconds: *deepseekTimeout,
		CDPURL:                 *cdpURL,
		CDPURL2:                *cdpURL2,
		TimeoutMS:              *timeoutMS,
		WaitMS:                 *waitMS,
	}
	output, runErr := aibalance.Run(ctx, selectedServices, options, makeProgressEmitter(*progressJSONL))
	if runErr != nil {
		fmt.Fprintf(os.Stderr, "%v\n", runErr)
		os.Exit(2)
	}

	summary := aibalance.SummarizeOutput(output)
	switch {
	case *debugOutput:
		printJSON(aibalance.ScrubDebugOutput(output))
	case *jsonOutput:
		printJSON(summary)
	default:
		printHumanSummary(summary)
	}

	if *strictExit {
		accounts, _ := output["accounts"].(map[string]any)
		for _, serviceName := range selectedServices {
			serviceResult, _ := accounts[serviceName].(map[string]any)
			status, _ := serviceResult["status"].(string)
			switch status {
			case "ok", "partial", "skipped":
			default:
				os.Exit(1)
			}
		}
	}
}

// makeProgressEmitter returns a stdout JSONL emitter, or nil when disabled.
func makeProgressEmitter(enabled bool) aibalance.ProgressEmitter {
	if !enabled {
		return nil
	}
	return func(event string, payload map[string]any) {
		message := map[string]any{"event": event}
		for payloadKey, payloadValue := range payload {
			message[payloadKey] = payloadValue
		}
		encoded, encodeErr := json.Marshal(message)
		if encodeErr != nil {
			return
		}
		fmt.Println(string(encoded))
	}
}

// printJSON writes an indented JSON document to stdout.
func printJSON(value any) {
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	encoder.SetEscapeHTML(false)
	if encodeErr := encoder.Encode(value); encodeErr != nil {
		fmt.Fprintf(os.Stderr, "encode output: %v\n", encodeErr)
		os.Exit(1)
	}
}

// printHumanSummary renders one block per service for interactive use,
// sharing the view layer with the TUI.
func printHumanSummary(summary map[string]any) {
	generatedAt, _ := summary["generated_at"].(string)
	if generatedAt != "" {
		fmt.Printf("Generated at %s\n", generatedAt)
	}
	for _, view := range aibalance.FormatSummaryViews(summary) {
		if view.Status != "OK" {
			fmt.Printf("%-18s %-12s %s\n", view.Name, view.Status, view.Detail)
			continue
		}
		fmt.Printf("%-18s %s\n", view.Name, view.Status)
		for _, quota := range view.Quotas {
			fmt.Printf("  %-16s %v/%v | %v%% left | reset %s\n",
				quota.Label, quota.Remaining, quota.Limit, quota.PercentLeft, quota.Reset)
		}
		for _, fact := range view.Facts {
			fmt.Printf("  %s\n", fact)
		}
	}
}
