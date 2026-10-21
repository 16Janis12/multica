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

var sprintCmd = &cobra.Command{
	Use:   "sprint",
	Short: "Work with sprints",
}

var sprintListCmd = &cobra.Command{
	Use:   "list",
	Short: "List sprints in the workspace",
	RunE:  runSprintList,
}

var sprintGetCmd = &cobra.Command{
	Use:   "get <id>",
	Short: "Get sprint details",
	Args:  exactArgs(1),
	RunE:  runSprintGet,
}

var sprintCreateCmd = &cobra.Command{
	Use:   "create",
	Short: "Create a new sprint",
	RunE:  runSprintCreate,
}

var sprintStartCmd = &cobra.Command{
	Use:   "start <id>",
	Short: "Start a sprint (sets status to active)",
	Args:  exactArgs(1),
	RunE:  runSprintStart,
}

var sprintCompleteCmd = &cobra.Command{
	Use:   "complete <id>",
	Short: "Complete a sprint (moves incomplete issues to target sprint or backlog)",
	Args:  exactArgs(1),
	RunE:  runSprintComplete,
}

var sprintDeleteCmd = &cobra.Command{
	Use:   "delete <id>",
	Short: "Delete a sprint",
	Args:  exactArgs(1),
	RunE:  runSprintDelete,
}

var validSprintStatuses = []string{
	"planned", "active", "completed", "cancelled",
}

func validateSprintStatus(status string) error {
	for _, s := range validSprintStatuses {
		if s == status {
			return nil
		}
	}
	return fmt.Errorf("invalid status %q; valid values: %s", status, strings.Join(validSprintStatuses, ", "))
}

func init() {
	sprintCmd.AddCommand(sprintListCmd)
	sprintCmd.AddCommand(sprintGetCmd)
	sprintCmd.AddCommand(sprintCreateCmd)
	sprintCmd.AddCommand(sprintStartCmd)
	sprintCmd.AddCommand(sprintCompleteCmd)
	sprintCmd.AddCommand(sprintDeleteCmd)

	// sprint list
	sprintListCmd.Flags().String("output", "table", "Output format: table or json")
	sprintListCmd.Flags().Bool("full-id", false, "Show full UUIDs in table output")
	sprintListCmd.Flags().String("status", "", "Filter by status")
	sprintListCmd.Flags().String("project", "", "Filter by project ID")

	// sprint get
	sprintGetCmd.Flags().String("output", "json", "Output format: table or json")

	// sprint create
	sprintCreateCmd.Flags().String("name", "", "Sprint name (required)")
	sprintCreateCmd.Flags().String("goal", "", "Sprint goal")
	sprintCreateCmd.Flags().String("project", "", "Project ID")
	sprintCreateCmd.Flags().String("start-date", "", "Start date (calendar day, YYYY-MM-DD, required)")
	sprintCreateCmd.Flags().String("end-date", "", "End date (calendar day, YYYY-MM-DD, required)")
	sprintCreateCmd.Flags().String("output", "json", "Output format: table or json")

	// sprint start
	sprintStartCmd.Flags().String("output", "json", "Output format: table or json")

	// sprint complete
	sprintCompleteCmd.Flags().String("target-sprint", "", "Target sprint ID to move incomplete issues (omit to move to backlog)")
	sprintCompleteCmd.Flags().String("output", "json", "Output format: table or json")

	// sprint delete
	sprintDeleteCmd.Flags().String("output", "json", "Output format: table or json")
}

func runSprintList(cmd *cobra.Command, _ []string) error {
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

	path := "/api/sprints"
	if len(params) > 0 {
		path += "?" + params.Encode()
	}

	var result map[string]any
	if err := client.GetJSON(ctx, path, &result); err != nil {
		return fmt.Errorf("list sprints: %w", err)
	}

	output, _ := cmd.Flags().GetString("output")
	if output == "table" {
		fullID, _ := cmd.Flags().GetBool("full-id")
		sprints, _ := result["sprints"].([]any)
		headers := []string{"NAME", "STATUS", "START DATE", "END DATE", "GOAL"}
		if fullID {
			headers = append([]string{"ID"}, headers...)
		}
		rows := make([][]string, len(sprints))
		for i, raw := range sprints {
			sprint, _ := raw.(map[string]any)
			row := []string{
				strVal(sprint, "name"),
				strVal(sprint, "status"),
				strVal(sprint, "start_date"),
				strVal(sprint, "end_date"),
				strVal(sprint, "goal"),
			}
			if fullID {
				row = append([]string{strVal(sprint, "id")}, row...)
			}
			rows[i] = row
		}
		cli.PrintTable(os.Stdout, headers, rows)
		return nil
	}

	return cli.PrintJSON(os.Stdout, result)
}

func runSprintGet(cmd *cobra.Command, args []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}

	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()

	var sprint map[string]any
	if err := client.GetJSON(ctx, "/api/sprints/"+args[0], &sprint); err != nil {
		return fmt.Errorf("get sprint: %w", err)
	}

	output, _ := cmd.Flags().GetString("output")
	if output == "table" {
		headers := []string{"ID", "NAME", "STATUS", "START DATE", "END DATE"}
		rows := [][]string{{
			strVal(sprint, "id"),
			strVal(sprint, "name"),
			strVal(sprint, "status"),
			strVal(sprint, "start_date"),
			strVal(sprint, "end_date"),
		}}
		cli.PrintTable(os.Stdout, headers, rows)
		return nil
	}

	return cli.PrintJSON(os.Stdout, sprint)
}

func runSprintCreate(cmd *cobra.Command, _ []string) error {
	name, _ := cmd.Flags().GetString("name")
	if name == "" {
		return fmt.Errorf("--name is required")
	}
	startDate, _ := cmd.Flags().GetString("start-date")
	if startDate == "" {
		return fmt.Errorf("--start-date is required")
	}
	endDate, _ := cmd.Flags().GetString("end-date")
	if endDate == "" {
		return fmt.Errorf("--end-date is required")
	}

	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}

	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()

	body := map[string]any{
		"name":       name,
		"start_date": startDate,
		"end_date":   endDate,
	}
	if v, _ := cmd.Flags().GetString("goal"); v != "" {
		body["goal"] = v
	}
	if v, _ := cmd.Flags().GetString("project"); v != "" {
		body["project_id"] = v
	}

	var sprint map[string]any
	if err := client.PostJSON(ctx, "/api/sprints", body, &sprint); err != nil {
		return fmt.Errorf("create sprint: %w", err)
	}

	output, _ := cmd.Flags().GetString("output")
	if output == "table" {
		headers := []string{"ID", "NAME", "STATUS", "START DATE", "END DATE"}
		rows := [][]string{{
			strVal(sprint, "id"),
			strVal(sprint, "name"),
			strVal(sprint, "status"),
			strVal(sprint, "start_date"),
			strVal(sprint, "end_date"),
		}}
		cli.PrintTable(os.Stdout, headers, rows)
		return nil
	}

	return cli.PrintJSON(os.Stdout, sprint)
}

func runSprintStart(cmd *cobra.Command, args []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}

	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()

	var sprint map[string]any
	if err := client.PostJSON(ctx, "/api/sprints/"+args[0]+"/start", map[string]any{}, &sprint); err != nil {
		return fmt.Errorf("start sprint: %w", err)
	}

	output, _ := cmd.Flags().GetString("output")
	if output == "table" {
		headers := []string{"ID", "NAME", "STATUS", "START DATE", "END DATE"}
		rows := [][]string{{
			strVal(sprint, "id"),
			strVal(sprint, "name"),
			strVal(sprint, "status"),
			strVal(sprint, "start_date"),
			strVal(sprint, "end_date"),
		}}
		cli.PrintTable(os.Stdout, headers, rows)
		return nil
	}

	return cli.PrintJSON(os.Stdout, sprint)
}

func runSprintComplete(cmd *cobra.Command, args []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}

	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()

	body := map[string]any{}
	if v, _ := cmd.Flags().GetString("target-sprint"); v != "" {
		body["target_sprint_id"] = v
	}

	var sprint map[string]any
	if err := client.PostJSON(ctx, "/api/sprints/"+args[0]+"/complete", body, &sprint); err != nil {
		return fmt.Errorf("complete sprint: %w", err)
	}

	output, _ := cmd.Flags().GetString("output")
	if output == "table" {
		headers := []string{"ID", "NAME", "STATUS", "START DATE", "END DATE"}
		rows := [][]string{{
			strVal(sprint, "id"),
			strVal(sprint, "name"),
			strVal(sprint, "status"),
			strVal(sprint, "start_date"),
			strVal(sprint, "end_date"),
		}}
		cli.PrintTable(os.Stdout, headers, rows)
		return nil
	}

	return cli.PrintJSON(os.Stdout, sprint)
}

func runSprintDelete(cmd *cobra.Command, args []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}

	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()

	var result map[string]any
	if err := client.DeleteJSONResponse(ctx, "/api/sprints/"+args[0], &result); err != nil {
		return fmt.Errorf("delete sprint: %w", err)
	}

	output, _ := cmd.Flags().GetString("output")
	if output == "table" {
		fmt.Fprintf(os.Stdout, "Deleted sprint %s\n", args[0])
		return nil
	}

	return cli.PrintJSON(os.Stdout, result)
}
