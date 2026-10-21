package main

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/multica-ai/multica/server/internal/cli"
)

var epicCmd = &cobra.Command{
	Use:   "epic",
	Short: "Work with epics",
}

var epicListCmd = &cobra.Command{
	Use:   "list",
	Short: "List epics in the workspace",
	RunE:  runEpicList,
}

var epicGetCmd = &cobra.Command{
	Use:   "get <id>",
	Short: "Get epic details",
	Args:  exactArgs(1),
	RunE:  runEpicGet,
}

var epicCreateCmd = &cobra.Command{
	Use:   "create",
	Short: "Create a new epic",
	RunE:  runEpicCreate,
}

var epicUpdateCmd = &cobra.Command{
	Use:   "update <id>",
	Short: "Update an epic",
	Args:  exactArgs(1),
	RunE:  runEpicUpdate,
}

var epicDeleteCmd = &cobra.Command{
	Use:   "delete <id>",
	Short: "Delete an epic",
	Args:  exactArgs(1),
	RunE:  runEpicDelete,
}

var validEpicStatuses = []string{
	"planned", "in_progress", "completed", "cancelled",
}

func validateEpicStatus(status string) error {
	for _, s := range validEpicStatuses {
		if s == status {
			return nil
		}
	}
	return fmt.Errorf("invalid status %q; valid values: %s", status, strings.Join(validEpicStatuses, ", "))
}

func init() {
	epicCmd.AddCommand(epicListCmd)
	epicCmd.AddCommand(epicGetCmd)
	epicCmd.AddCommand(epicCreateCmd)
	epicCmd.AddCommand(epicUpdateCmd)
	epicCmd.AddCommand(epicDeleteCmd)

	// epic list
	epicListCmd.Flags().String("output", "table", "Output format: table or json")
	epicListCmd.Flags().Bool("full-id", false, "Show full UUIDs in table output")
	epicListCmd.Flags().String("status", "", "Filter by status")
	epicListCmd.Flags().String("project", "", "Filter by project ID")

	// epic get
	epicGetCmd.Flags().String("output", "json", "Output format: table or json")

	// epic create
	epicCreateCmd.Flags().String("title", "", "Epic title (required)")
	epicCreateCmd.Flags().String("description", "", "Epic description")
	epicCreateCmd.Flags().String("color", "#6366f1", "Epic color")
	epicCreateCmd.Flags().String("status", "planned", "Epic status")
	epicCreateCmd.Flags().String("project", "", "Project ID")
	epicCreateCmd.Flags().String("lead", "", "Lead name (member or agent)")
	epicCreateCmd.Flags().String("start-date", "", "Start date (calendar day, YYYY-MM-DD)")
	epicCreateCmd.Flags().String("due-date", "", "Due date (calendar day, YYYY-MM-DD)")
	epicCreateCmd.Flags().String("output", "json", "Output format: table or json")

	// epic update
	epicUpdateCmd.Flags().String("title", "", "New title")
	epicUpdateCmd.Flags().String("description", "", "New description")
	epicUpdateCmd.Flags().String("color", "", "New color")
	epicUpdateCmd.Flags().String("status", "", "New status")
	epicUpdateCmd.Flags().String("project", "", "New project ID (pass empty string to clear)")
	epicUpdateCmd.Flags().String("lead", "", "New lead name (member or agent)")
	epicUpdateCmd.Flags().String("start-date", "", "New start date (calendar day, YYYY-MM-DD; pass empty string to clear)")
	epicUpdateCmd.Flags().String("due-date", "", "New due date (calendar day, YYYY-MM-DD; pass empty string to clear)")
	epicUpdateCmd.Flags().String("output", "json", "Output format: table or json")

	// epic delete
	epicDeleteCmd.Flags().String("output", "json", "Output format: table or json")
}

func runEpicList(cmd *cobra.Command, _ []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}

	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()

	params := url.Values{}
	if client.WorkspaceID != "" {
		params.Set("workspace_id", client.WorkspaceID)
	}
	if v, _ := cmd.Flags().GetString("status"); v != "" {
		params.Set("status", v)
	}
	if v, _ := cmd.Flags().GetString("project"); v != "" {
		params.Set("project_id", v)
	}

	path := "/api/epics"
	if len(params) > 0 {
		path += "?" + params.Encode()
	}

	var result map[string]any
	if err := client.GetJSON(ctx, path, &result); err != nil {
		return fmt.Errorf("list epics: %w", err)
	}

	output, _ := cmd.Flags().GetString("output")
	if output == "table" {
		fullID, _ := cmd.Flags().GetBool("full-id")
		epics, _ := result["epics"].([]any)
		headers := []string{"IDENTIFIER", "TITLE", "STATUS", "COLOR", "START DATE", "DUE DATE"}
		if fullID {
			headers = append([]string{"ID"}, headers...)
		}
		rows := make([][]string, len(epics))
		for i, raw := range epics {
			epic, _ := raw.(map[string]any)
			row := []string{
				strVal(epic, "identifier"),
				strVal(epic, "title"),
				strVal(epic, "status"),
				strVal(epic, "color"),
				strVal(epic, "start_date"),
				strVal(epic, "due_date"),
			}
			if fullID {
				row = append([]string{strVal(epic, "id")}, row...)
			}
			rows[i] = row
		}
		cli.PrintTable(os.Stdout, headers, rows)
		return nil
	}

	return cli.PrintJSON(os.Stdout, result)
}

func runEpicGet(cmd *cobra.Command, args []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}

	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()

	var epic map[string]any
	if err := client.GetJSON(ctx, "/api/epics/"+args[0], &epic); err != nil {
		return fmt.Errorf("get epic: %w", err)
	}

	output, _ := cmd.Flags().GetString("output")
	if output == "table" {
		headers := []string{"ID", "IDENTIFIER", "TITLE", "STATUS", "COLOR"}
		rows := [][]string{{
			strVal(epic, "id"),
			strVal(epic, "identifier"),
			strVal(epic, "title"),
			strVal(epic, "status"),
			strVal(epic, "color"),
		}}
		cli.PrintTable(os.Stdout, headers, rows)
		return nil
	}

	return cli.PrintJSON(os.Stdout, epic)
}

func runEpicCreate(cmd *cobra.Command, _ []string) error {
	title, _ := cmd.Flags().GetString("title")
	if title == "" {
		return fmt.Errorf("--title is required")
	}

	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}

	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()

	body := map[string]any{"title": title}
	if v, _ := cmd.Flags().GetString("description"); v != "" {
		body["description"] = v
	}
	if v, _ := cmd.Flags().GetString("color"); v != "" {
		body["color"] = v
	}
	if v, _ := cmd.Flags().GetString("status"); v != "" {
		if err := validateEpicStatus(v); err != nil {
			return err
		}
		body["status"] = v
	}
	if v, _ := cmd.Flags().GetString("project"); v != "" {
		body["project_id"] = v
	}
	if v, _ := cmd.Flags().GetString("lead"); v != "" {
		aType, aID, resolveErr := resolveAssignee(ctx, client, v, memberOrAgentKinds)
		if resolveErr != nil {
			return fmt.Errorf("resolve lead: %w", resolveErr)
		}
		body["lead_type"] = aType
		body["lead_id"] = aID
	}
	if v, _ := cmd.Flags().GetString("start-date"); v != "" {
		body["start_date"] = v
	}
	if v, _ := cmd.Flags().GetString("due-date"); v != "" {
		body["due_date"] = v
	}

	var epic map[string]any
	if err := client.PostJSON(ctx, "/api/epics", body, &epic); err != nil {
		return fmt.Errorf("create epic: %w", err)
	}

	output, _ := cmd.Flags().GetString("output")
	if output == "table" {
		headers := []string{"ID", "IDENTIFIER", "TITLE", "STATUS"}
		rows := [][]string{{
			strVal(epic, "id"),
			strVal(epic, "identifier"),
			strVal(epic, "title"),
			strVal(epic, "status"),
		}}
		cli.PrintTable(os.Stdout, headers, rows)
		return nil
	}

	return cli.PrintJSON(os.Stdout, epic)
}

func runEpicUpdate(cmd *cobra.Command, args []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}

	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()

	body := map[string]any{}
	if cmd.Flags().Changed("title") {
		v, _ := cmd.Flags().GetString("title")
		body["title"] = v
	}
	if cmd.Flags().Changed("description") {
		v, _ := cmd.Flags().GetString("description")
		body["description"] = v
	}
	if cmd.Flags().Changed("color") {
		v, _ := cmd.Flags().GetString("color")
		body["color"] = v
	}
	if cmd.Flags().Changed("status") {
		v, _ := cmd.Flags().GetString("status")
		if err := validateEpicStatus(v); err != nil {
			return err
		}
		body["status"] = v
	}
	if cmd.Flags().Changed("project") {
		v, _ := cmd.Flags().GetString("project")
		if v == "" {
			body["project_id"] = nil
		} else {
			body["project_id"] = v
		}
	}
	if cmd.Flags().Changed("lead") {
		v, _ := cmd.Flags().GetString("lead")
		if v == "" {
			body["lead_type"] = nil
			body["lead_id"] = nil
		} else {
			aType, aID, resolveErr := resolveAssignee(ctx, client, v, memberOrAgentKinds)
			if resolveErr != nil {
				return fmt.Errorf("resolve lead: %w", resolveErr)
			}
			body["lead_type"] = aType
			body["lead_id"] = aID
		}
	}
	if cmd.Flags().Changed("start-date") {
		v, _ := cmd.Flags().GetString("start-date")
		if v == "" {
			body["start_date"] = nil
		} else {
			body["start_date"] = v
		}
	}
	if cmd.Flags().Changed("due-date") {
		v, _ := cmd.Flags().GetString("due-date")
		if v == "" {
			body["due_date"] = nil
		} else {
			body["due_date"] = v
		}
	}

	var epic map[string]any
	if err := client.PutJSON(ctx, "/api/epics/"+args[0], body, &epic); err != nil {
		return fmt.Errorf("update epic: %w", err)
	}

	output, _ := cmd.Flags().GetString("output")
	if output == "table" {
		headers := []string{"ID", "IDENTIFIER", "TITLE", "STATUS"}
		rows := [][]string{{
			strVal(epic, "id"),
			strVal(epic, "identifier"),
			strVal(epic, "title"),
			strVal(epic, "status"),
		}}
		cli.PrintTable(os.Stdout, headers, rows)
		return nil
	}

	return cli.PrintJSON(os.Stdout, epic)
}

func runEpicDelete(cmd *cobra.Command, args []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}

	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()

	var result map[string]any
	if err := client.DeleteJSONResponse(ctx, "/api/epics/"+args[0], &result); err != nil {
		return fmt.Errorf("delete epic: %w", err)
	}

	output, _ := cmd.Flags().GetString("output")
	if output == "table" {
		fmt.Fprintf(os.Stdout, "Deleted epic %s\n", args[0])
		return nil
	}

	return cli.PrintJSON(os.Stdout, result)
}
