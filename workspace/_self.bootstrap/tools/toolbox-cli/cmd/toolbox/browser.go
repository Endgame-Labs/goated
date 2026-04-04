package main

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"toolbox-example/internal/creds"
	"toolbox-example/internal/httputil"

	"github.com/spf13/cobra"
)

const browserUseBaseURL = "https://api.browser-use.com/api/v3"

func browserAuth() (map[string]string, error) {
	key, err := creds.Get("BROWSER_USE_API_KEY")
	if err != nil {
		return nil, fmt.Errorf("get BROWSER_USE_API_KEY via goat creds: %w", err)
	}
	return map[string]string{"X-Browser-Use-API-Key": key}, nil
}

func browserCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "browser",
		Short: "Browser Use cloud browser automation",
	}
	cmd.AddCommand(browserRunCmd())
	cmd.AddCommand(browserStatusCmd())
	cmd.AddCommand(browserResultCmd())
	cmd.AddCommand(browserStopCmd())
	cmd.AddCommand(browserListCmd())
	cmd.AddCommand(browserMessagesCmd())
	cmd.AddCommand(browserDeleteCmd())
	cmd.AddCommand(browserShareCmd())
	cmd.AddCommand(browserTasksCmd())
	cmd.AddCommand(browserProfilesCmd())
	cmd.AddCommand(browserBillingCmd())
	return cmd
}

type browserSessionResult struct {
	ID                string  `json:"id"`
	Status            string  `json:"status"`
	Model             string  `json:"model"`
	Title             string  `json:"title"`
	Output            any     `json:"output"`
	LiveURL           string  `json:"live_url"`
	TotalInputTokens  int     `json:"total_input_tokens"`
	TotalOutputTokens int     `json:"total_output_tokens"`
	LLMCostUSD        float64 `json:"llm_cost_usd"`
	ProxyCostUSD      float64 `json:"proxy_cost_usd"`
	ProxyUsedMB       float64 `json:"proxy_used_mb"`
	TotalCostUSD      float64 `json:"total_cost_usd"`
	CreatedAt         string  `json:"created_at"`
	UpdatedAt         string  `json:"updated_at"`
}

func printBrowserSession(s browserSessionResult) {
	fmt.Printf("Session ID: %s\n", s.ID)
	fmt.Printf("Status:     %s\n", s.Status)
	if s.Title != "" {
		fmt.Printf("Title:      %s\n", s.Title)
	}
	if s.Model != "" {
		fmt.Printf("Model:      %s\n", s.Model)
	}
	if s.LiveURL != "" {
		fmt.Printf("Live URL:   %s\n", s.LiveURL)
	}
	if s.TotalCostUSD > 0 {
		fmt.Printf("Cost:       $%.4f (LLM: $%.4f, Proxy: $%.4f)\n", s.TotalCostUSD, s.LLMCostUSD, s.ProxyCostUSD)
	}
	if s.TotalInputTokens > 0 || s.TotalOutputTokens > 0 {
		fmt.Printf("Tokens:     %d in / %d out\n", s.TotalInputTokens, s.TotalOutputTokens)
	}
	if s.CreatedAt != "" {
		fmt.Printf("Created:    %s\n", s.CreatedAt)
	}
	if s.Output != nil {
		out, _ := json.MarshalIndent(s.Output, "", "  ")
		if string(out) != "null" && string(out) != `""` {
			fmt.Printf("Output:\n%s\n", string(out))
		}
	}
}

func browserRunCmd() *cobra.Command {
	var task, model, sessionID, profileID, proxyCountry, outputSchema string
	var keepAlive, wait bool
	var maxCost float64
	var pollInterval int

	cmd := &cobra.Command{
		Use:   "run --task \"description of what to do\"",
		Short: "Run a browser automation task",
		RunE: func(cmd *cobra.Command, args []string) error {
			if task == "" {
				return fmt.Errorf("--task is required")
			}
			auth, err := browserAuth()
			if err != nil {
				return err
			}

			body := map[string]any{"task": task}
			if model != "" {
				body["model"] = model
			}
			if sessionID != "" {
				body["session_id"] = sessionID
			}
			if keepAlive {
				body["keep_alive"] = true
			}
			if maxCost > 0 {
				body["max_cost_usd"] = maxCost
			}
			if profileID != "" {
				body["profile_id"] = profileID
			}
			if proxyCountry != "" {
				body["proxy_country_code"] = proxyCountry
			}
			if outputSchema != "" {
				var schema map[string]any
				if err := json.Unmarshal([]byte(outputSchema), &schema); err != nil {
					return fmt.Errorf("invalid --output-schema JSON: %w", err)
				}
				body["output_schema"] = schema
			}

			var result browserSessionResult
			status, err := httputil.PostJSON(browserUseBaseURL+"/sessions", body, auth, &result)
			if err != nil {
				return fmt.Errorf("create session: %w", err)
			}
			if status >= 400 {
				return fmt.Errorf("API returned status %d", status)
			}
			printBrowserSession(result)

			if wait && result.ID != "" {
				fmt.Println("\nWaiting for task to complete...")
				interval := time.Duration(pollInterval) * time.Second
				for {
					time.Sleep(interval)
					var pollResult browserSessionResult
					st, err := httputil.GetJSON(browserUseBaseURL+"/sessions/"+result.ID, auth, &pollResult)
					if err != nil {
						fmt.Printf("  poll error: %v\n", err)
						continue
					}
					if st >= 400 {
						fmt.Printf("  poll returned status %d\n", st)
						continue
					}
					fmt.Printf("  [%s] status: %s\n", time.Now().Format("15:04:05"), pollResult.Status)
					switch pollResult.Status {
					case "stopped", "error", "timed_out", "idle":
						fmt.Println("\nFinal result:")
						printBrowserSession(pollResult)
						return nil
					}
				}
			}
			return nil
		},
	}

	cmd.Flags().StringVar(&task, "task", "", "Task description (required)")
	cmd.Flags().StringVar(&model, "model", "bu-mini", "Model: bu-mini or bu-max")
	cmd.Flags().StringVar(&sessionID, "session-id", "", "Dispatch to existing idle session")
	cmd.Flags().BoolVar(&keepAlive, "keep-alive", false, "Keep session alive after task completes")
	cmd.Flags().Float64Var(&maxCost, "max-cost", 0, "Maximum cost in USD")
	cmd.Flags().StringVar(&profileID, "profile-id", "", "Browser profile ID for persistent state")
	cmd.Flags().StringVar(&proxyCountry, "proxy-country", "", "Proxy country code")
	cmd.Flags().StringVar(&outputSchema, "output-schema", "", "JSON schema for structured output")
	cmd.Flags().BoolVar(&wait, "wait", false, "Poll until task completes")
	cmd.Flags().IntVar(&pollInterval, "poll-interval", 5, "Poll interval in seconds")
	return cmd
}

func browserStatusCmd() *cobra.Command {
	var sessionID string
	cmd := &cobra.Command{
		Use:   "status --session-id <id>",
		Short: "Get session status and details",
		RunE: func(cmd *cobra.Command, args []string) error {
			if sessionID == "" {
				return fmt.Errorf("--session-id is required")
			}
			auth, err := browserAuth()
			if err != nil {
				return err
			}
			var result browserSessionResult
			status, err := httputil.GetJSON(browserUseBaseURL+"/sessions/"+sessionID, auth, &result)
			if err != nil {
				return fmt.Errorf("get session: %w", err)
			}
			if status >= 400 {
				return fmt.Errorf("API returned status %d", status)
			}
			printBrowserSession(result)
			return nil
		},
	}
	cmd.Flags().StringVar(&sessionID, "session-id", "", "Session ID (required)")
	return cmd
}

func browserResultCmd() *cobra.Command {
	var taskID string
	cmd := &cobra.Command{
		Use:   "result --task-id <id>",
		Short: "Get task result and output files",
		RunE: func(cmd *cobra.Command, args []string) error {
			if taskID == "" {
				return fmt.Errorf("--task-id is required")
			}
			auth, err := browserAuth()
			if err != nil {
				return err
			}
			var result struct {
				ID          string `json:"id"`
				Status      string `json:"status"`
				Output      any    `json:"output"`
				OutputFiles []struct {
					Name string `json:"name"`
					URL  string `json:"url"`
				} `json:"output_files"`
			}
			status, err := httputil.GetJSON(browserUseBaseURL+"/tasks/"+taskID, auth, &result)
			if err != nil {
				return fmt.Errorf("get task: %w", err)
			}
			if status >= 400 {
				return fmt.Errorf("API returned status %d", status)
			}
			fmt.Printf("Task ID: %s\n", result.ID)
			fmt.Printf("Status:  %s\n", result.Status)
			if result.Output != nil {
				out, _ := json.MarshalIndent(result.Output, "", "  ")
				if string(out) != "null" {
					fmt.Printf("Output:\n%s\n", string(out))
				}
			}
			if len(result.OutputFiles) > 0 {
				fmt.Println("Output Files:")
				for _, f := range result.OutputFiles {
					fmt.Printf("  %s: %s\n", f.Name, f.URL)
				}
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&taskID, "task-id", "", "Task ID (required)")
	return cmd
}

func browserTasksCmd() *cobra.Command {
	var limit int
	var cursor string
	cmd := &cobra.Command{
		Use:   "tasks",
		Short: "List tasks",
		RunE: func(cmd *cobra.Command, args []string) error {
			auth, err := browserAuth()
			if err != nil {
				return err
			}
			url := browserUseBaseURL + "/tasks"
			params := []string{}
			if limit > 0 {
				params = append(params, fmt.Sprintf("limit=%d", limit))
			}
			if cursor != "" {
				params = append(params, fmt.Sprintf("cursor=%s", cursor))
			}
			if len(params) > 0 {
				url += "?" + strings.Join(params, "&")
			}
			var result struct {
				Tasks []struct {
					ID     string `json:"id"`
					Status string `json:"status"`
					Output any    `json:"output"`
				} `json:"tasks"`
				NextCursor string `json:"next_cursor"`
				HasMore    bool   `json:"has_more"`
			}
			status, err := httputil.GetJSON(url, auth, &result)
			if err != nil {
				return fmt.Errorf("list tasks: %w", err)
			}
			if status >= 400 {
				return fmt.Errorf("API returned status %d", status)
			}
			if len(result.Tasks) == 0 {
				fmt.Println("No tasks found.")
				return nil
			}
			for _, t := range result.Tasks {
				fmt.Printf("ID: %s  Status: %s\n", t.ID, t.Status)
			}
			if result.HasMore {
				fmt.Printf("\nMore results available. Use --cursor %s\n", result.NextCursor)
			}
			return nil
		},
	}
	cmd.Flags().IntVar(&limit, "limit", 20, "Number of tasks to return")
	cmd.Flags().StringVar(&cursor, "cursor", "", "Pagination cursor")
	return cmd
}

func browserStopCmd() *cobra.Command {
	var sessionID string
	var strategy string
	cmd := &cobra.Command{
		Use:   "stop --session-id <id>",
		Short: "Stop a running session or task",
		RunE: func(cmd *cobra.Command, args []string) error {
			if sessionID == "" {
				return fmt.Errorf("--session-id is required")
			}
			auth, err := browserAuth()
			if err != nil {
				return err
			}
			body := map[string]any{}
			if strategy != "" {
				body["strategy"] = strategy
			}
			var result map[string]any
			status, err := httputil.PostJSON(browserUseBaseURL+"/sessions/"+sessionID+"/stop", body, auth, &result)
			if err != nil {
				return fmt.Errorf("stop session: %w", err)
			}
			if status >= 400 {
				return fmt.Errorf("API returned status %d", status)
			}
			fmt.Printf("Session %s stopped (strategy: %s)\n", sessionID, strategy)
			return nil
		},
	}
	cmd.Flags().StringVar(&sessionID, "session-id", "", "Session ID (required)")
	cmd.Flags().StringVar(&strategy, "strategy", "task", "Stop strategy: task or session")
	return cmd
}

func browserListCmd() *cobra.Command {
	var limit int
	var cursor string
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List sessions",
		RunE: func(cmd *cobra.Command, args []string) error {
			auth, err := browserAuth()
			if err != nil {
				return err
			}
			url := browserUseBaseURL + "/sessions"
			params := []string{}
			if limit > 0 {
				params = append(params, fmt.Sprintf("limit=%d", limit))
			}
			if cursor != "" {
				params = append(params, fmt.Sprintf("cursor=%s", cursor))
			}
			if len(params) > 0 {
				url += "?" + strings.Join(params, "&")
			}
			var result struct {
				Sessions   []browserSessionResult `json:"sessions"`
				NextCursor string                 `json:"next_cursor"`
				HasMore    bool                   `json:"has_more"`
			}
			status, err := httputil.GetJSON(url, auth, &result)
			if err != nil {
				return fmt.Errorf("list sessions: %w", err)
			}
			if status >= 400 {
				return fmt.Errorf("API returned status %d", status)
			}
			if len(result.Sessions) == 0 {
				fmt.Println("No sessions found.")
				return nil
			}
			for i, s := range result.Sessions {
				if i > 0 {
					fmt.Println("---")
				}
				fmt.Printf("ID: %s  Status: %s  Model: %s", s.ID, s.Status, s.Model)
				if s.Title != "" {
					fmt.Printf("  Title: %s", s.Title)
				}
				if s.TotalCostUSD > 0 {
					fmt.Printf("  Cost: $%.4f", s.TotalCostUSD)
				}
				fmt.Println()
			}
			if result.HasMore {
				fmt.Printf("\nMore results available. Use --cursor %s\n", result.NextCursor)
			}
			return nil
		},
	}
	cmd.Flags().IntVar(&limit, "limit", 20, "Number of sessions to return")
	cmd.Flags().StringVar(&cursor, "cursor", "", "Pagination cursor")
	return cmd
}

func browserMessagesCmd() *cobra.Command {
	var sessionID string
	var limit int
	var cursor string
	cmd := &cobra.Command{
		Use:   "messages --session-id <id>",
		Short: "Get message history for a session",
		RunE: func(cmd *cobra.Command, args []string) error {
			if sessionID == "" {
				return fmt.Errorf("--session-id is required")
			}
			auth, err := browserAuth()
			if err != nil {
				return err
			}
			body := map[string]any{}
			if limit > 0 {
				body["limit"] = limit
			}
			if cursor != "" {
				body["cursor"] = cursor
			}
			var result struct {
				Messages []struct {
					Role    string `json:"role"`
					Content string `json:"content"`
				} `json:"messages"`
				NextCursor string `json:"next_cursor"`
				HasMore    bool   `json:"has_more"`
			}
			status, err := httputil.PostJSON(browserUseBaseURL+"/sessions/"+sessionID+"/messages", body, auth, &result)
			if err != nil {
				return fmt.Errorf("get messages: %w", err)
			}
			if status >= 400 {
				return fmt.Errorf("API returned status %d", status)
			}
			if len(result.Messages) == 0 {
				fmt.Println("No messages found.")
				return nil
			}
			for _, m := range result.Messages {
				fmt.Printf("[%s] %s\n", m.Role, m.Content)
			}
			if result.HasMore {
				fmt.Printf("\nMore messages available. Use --cursor %s\n", result.NextCursor)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&sessionID, "session-id", "", "Session ID (required)")
	cmd.Flags().IntVar(&limit, "limit", 50, "Number of messages to return")
	cmd.Flags().StringVar(&cursor, "cursor", "", "Pagination cursor")
	return cmd
}

func browserDeleteCmd() *cobra.Command {
	var sessionID string
	cmd := &cobra.Command{
		Use:   "delete --session-id <id>",
		Short: "Delete a session permanently",
		RunE: func(cmd *cobra.Command, args []string) error {
			if sessionID == "" {
				return fmt.Errorf("--session-id is required")
			}
			auth, err := browserAuth()
			if err != nil {
				return err
			}
			var result map[string]any
			status, err := httputil.DoJSON("DELETE", browserUseBaseURL+"/sessions/"+sessionID, nil, auth, &result)
			if err != nil {
				return fmt.Errorf("delete session: %w", err)
			}
			if status >= 400 {
				return fmt.Errorf("API returned status %d", status)
			}
			fmt.Printf("Session %s deleted.\n", sessionID)
			return nil
		},
	}
	cmd.Flags().StringVar(&sessionID, "session-id", "", "Session ID (required)")
	return cmd
}

func browserShareCmd() *cobra.Command {
	var sessionID string
	cmd := &cobra.Command{
		Use:   "share --session-id <id>",
		Short: "Create a shareable link for a session",
		RunE: func(cmd *cobra.Command, args []string) error {
			if sessionID == "" {
				return fmt.Errorf("--session-id is required")
			}
			auth, err := browserAuth()
			if err != nil {
				return err
			}
			var result struct {
				URL string `json:"url"`
			}
			status, err := httputil.PostJSON(browserUseBaseURL+"/sessions/"+sessionID+"/share", nil, auth, &result)
			if err != nil {
				return fmt.Errorf("share session: %w", err)
			}
			if status >= 400 {
				return fmt.Errorf("API returned status %d", status)
			}
			fmt.Printf("Shareable URL: %s\n", result.URL)
			return nil
		},
	}
	cmd.Flags().StringVar(&sessionID, "session-id", "", "Session ID (required)")
	return cmd
}

func browserProfilesCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "profiles",
		Short: "Manage browser profiles",
	}
	cmd.AddCommand(browserProfileListCmd())
	cmd.AddCommand(browserProfileCreateCmd())
	cmd.AddCommand(browserProfileDeleteCmd())
	cmd.AddCommand(browserProfileRenameCmd())
	return cmd
}

func browserProfileListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List browser profiles",
		RunE: func(cmd *cobra.Command, args []string) error {
			auth, err := browserAuth()
			if err != nil {
				return err
			}
			var result struct {
				Profiles []struct {
					ID        string `json:"id"`
					Name      string `json:"name"`
					CreatedAt string `json:"created_at"`
				} `json:"profiles"`
			}
			status, err := httputil.GetJSON(browserUseBaseURL+"/profiles", auth, &result)
			if err != nil {
				return fmt.Errorf("list profiles: %w", err)
			}
			if status >= 400 {
				return fmt.Errorf("API returned status %d", status)
			}
			if len(result.Profiles) == 0 {
				fmt.Println("No profiles found.")
				return nil
			}
			for _, p := range result.Profiles {
				fmt.Printf("ID: %s  Name: %s  Created: %s\n", p.ID, p.Name, p.CreatedAt)
			}
			return nil
		},
	}
}

func browserProfileCreateCmd() *cobra.Command {
	var name string
	cmd := &cobra.Command{
		Use:   "create --name <name>",
		Short: "Create a browser profile",
		RunE: func(cmd *cobra.Command, args []string) error {
			if name == "" {
				return fmt.Errorf("--name is required")
			}
			auth, err := browserAuth()
			if err != nil {
				return err
			}
			body := map[string]string{"name": name}
			var result struct {
				ID   string `json:"id"`
				Name string `json:"name"`
			}
			status, err := httputil.PostJSON(browserUseBaseURL+"/profiles", body, auth, &result)
			if err != nil {
				return fmt.Errorf("create profile: %w", err)
			}
			if status >= 400 {
				return fmt.Errorf("API returned status %d", status)
			}
			fmt.Printf("Profile created: ID=%s Name=%s\n", result.ID, result.Name)
			return nil
		},
	}
	cmd.Flags().StringVar(&name, "name", "", "Profile name (required)")
	return cmd
}

func browserProfileDeleteCmd() *cobra.Command {
	var profileID string
	cmd := &cobra.Command{
		Use:   "delete --profile-id <id>",
		Short: "Delete a browser profile",
		RunE: func(cmd *cobra.Command, args []string) error {
			if profileID == "" {
				return fmt.Errorf("--profile-id is required")
			}
			auth, err := browserAuth()
			if err != nil {
				return err
			}
			var result map[string]any
			status, err := httputil.DoJSON("DELETE", browserUseBaseURL+"/profiles/"+profileID, nil, auth, &result)
			if err != nil {
				return fmt.Errorf("delete profile: %w", err)
			}
			if status >= 400 {
				return fmt.Errorf("API returned status %d", status)
			}
			fmt.Printf("Profile %s deleted.\n", profileID)
			return nil
		},
	}
	cmd.Flags().StringVar(&profileID, "profile-id", "", "Profile ID (required)")
	return cmd
}

func browserProfileRenameCmd() *cobra.Command {
	var profileID, name string
	cmd := &cobra.Command{
		Use:   "rename --profile-id <id> --name <name>",
		Short: "Rename a browser profile",
		RunE: func(cmd *cobra.Command, args []string) error {
			if profileID == "" || name == "" {
				return fmt.Errorf("--profile-id and --name are required")
			}
			auth, err := browserAuth()
			if err != nil {
				return err
			}
			body := map[string]string{"name": name}
			var result map[string]any
			status, err := httputil.DoJSON("PATCH", browserUseBaseURL+"/profiles/"+profileID, body, auth, &result)
			if err != nil {
				return fmt.Errorf("rename profile: %w", err)
			}
			if status >= 400 {
				return fmt.Errorf("API returned status %d", status)
			}
			fmt.Printf("Profile %s renamed to %s.\n", profileID, name)
			return nil
		},
	}
	cmd.Flags().StringVar(&profileID, "profile-id", "", "Profile ID (required)")
	cmd.Flags().StringVar(&name, "name", "", "New name (required)")
	return cmd
}

func browserBillingCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "billing",
		Short: "Get account billing and usage info",
		RunE: func(cmd *cobra.Command, args []string) error {
			auth, err := browserAuth()
			if err != nil {
				return err
			}
			var result map[string]any
			status, err := httputil.GetJSON(browserUseBaseURL+"/billing/account", auth, &result)
			if err != nil {
				return fmt.Errorf("get billing: %w", err)
			}
			if status >= 400 {
				return fmt.Errorf("API returned status %d", status)
			}
			out, _ := json.MarshalIndent(result, "", "  ")
			fmt.Println(string(out))
			return nil
		},
	}
}
