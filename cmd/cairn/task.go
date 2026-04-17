package main

import (
	"os"
	"path/filepath"
	"time"

	"github.com/spf13/cobra"

	"github.com/ProductOfAmerica/cairn/internal/cairnerr"
	"github.com/ProductOfAmerica/cairn/internal/cli"
	"github.com/ProductOfAmerica/cairn/internal/db"
	"github.com/ProductOfAmerica/cairn/internal/events"
	"github.com/ProductOfAmerica/cairn/internal/repoid"
	"github.com/ProductOfAmerica/cairn/internal/task"
)

// openStateDB opens the state.db for the repo-containing cwd. Shared helper.
func openStateDB(app *App) (*db.DB, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return nil, err
	}
	id, err := repoid.Resolve(cwd)
	if err != nil {
		return nil, cairnerr.New(cairnerr.CodeBadInput, "not_a_git_repo", err.Error()).WithCause(err)
	}
	stateRoot := cli.ResolveStateRoot(app.Flags.StateRoot)
	dbPath := filepath.Join(stateRoot, id, "state.db")
	return db.Open(dbPath)
}

func newTaskCmd(app *App) *cobra.Command {
	root := &cobra.Command{Use: "task", Short: "Task lifecycle"}
	root.AddCommand(newTaskPlanCmd(app))
	root.AddCommand(newTaskListCmd(app))
	root.AddCommand(newTaskClaimCmd(app))
	root.AddCommand(newTaskHeartbeatCmd(app))
	root.AddCommand(newTaskReleaseCmd(app))
	root.AddCommand(newTaskCompleteCmd(app))
	return root
}

func newTaskPlanCmd(app *App) *cobra.Command {
	var specsRoot string
	cmd := &cobra.Command{
		Use:   "plan",
		Short: "Materialize specs into state",
		RunE: func(cmd *cobra.Command, _ []string) error {
			opID, err := app.Flags.ResolveOpID(app.IDs)
			if err != nil {
				cli.WriteEnvelope(cmd.OutOrStdout(), cli.Envelope{Kind: "task.plan", Err: err})
				os.Exit(cli.ExitCodeFor(err))
			}
			os.Exit(cli.Run(cmd.OutOrStdout(), "task.plan", opID, func() (any, error) {
				h, err := openStateDB(app)
				if err != nil {
					return nil, err
				}
				defer h.Close()
				var res any
				err = h.WithTx(cmd.Context(), func(tx *db.Tx) error {
					store := task.NewStore(tx, events.NewAppender(app.Clock), app.IDs, app.Clock)
					r, err := store.Plan(task.PlanInput{OpID: opID, SpecsRoot: specsRoot})
					res = r
					return err
				})
				return res, err
			}))
			return nil
		},
	}
	cmd.Flags().StringVar(&specsRoot, "path", "specs", "spec root")
	return cmd
}

func newTaskListCmd(app *App) *cobra.Command {
	var status string
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List tasks",
		RunE: func(cmd *cobra.Command, _ []string) error {
			os.Exit(cli.Run(cmd.OutOrStdout(), "task.list", "", func() (any, error) {
				h, err := openStateDB(app)
				if err != nil {
					return nil, err
				}
				defer h.Close()
				var list []task.TaskRow
				err = h.WithReadTx(cmd.Context(), func(tx *db.Tx) error {
					store := task.NewStore(tx, events.NewAppender(app.Clock), app.IDs, app.Clock)
					l, err := store.List(status)
					list = l
					return err
				})
				return map[string]any{"tasks": list}, err
			}))
			return nil
		},
	}
	cmd.Flags().StringVar(&status, "status", "", "filter by status")
	return cmd
}

func newTaskClaimCmd(app *App) *cobra.Command {
	var agent, ttl string
	cmd := &cobra.Command{
		Use:   "claim <task_id>",
		Short: "Acquire a lease on a task",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			opID, err := app.Flags.ResolveOpID(app.IDs)
			if err != nil {
				cli.WriteEnvelope(cmd.OutOrStdout(), cli.Envelope{Kind: "task.claim", Err: err})
				os.Exit(cli.ExitCodeFor(err))
			}
			os.Exit(cli.Run(cmd.OutOrStdout(), "task.claim", opID, func() (any, error) {
				dur, derr := time.ParseDuration(ttl)
				if derr != nil {
					return nil, cairnerr.New(cairnerr.CodeBadInput, "bad_input",
						"invalid --ttl").WithDetails(map[string]any{"flag": "--ttl", "value": ttl})
				}
				h, err := openStateDB(app)
				if err != nil {
					return nil, err
				}
				defer h.Close()
				var res task.ClaimResult
				err = h.WithTx(cmd.Context(), func(tx *db.Tx) error {
					store := task.NewStore(tx, events.NewAppender(app.Clock), app.IDs, app.Clock)
					r, err := store.Claim(task.ClaimInput{
						OpID: opID, TaskID: args[0], AgentID: agent,
						TTLMs: dur.Milliseconds(),
					})
					res = r
					return err
				})
				return res, err
			}))
			return nil
		},
	}
	cmd.Flags().StringVar(&agent, "agent", "", "agent identifier (required)")
	cmd.Flags().StringVar(&ttl, "ttl", "30m", "lease duration (Go duration)")
	_ = cmd.MarkFlagRequired("agent")
	return cmd
}

func newTaskHeartbeatCmd(app *App) *cobra.Command {
	return &cobra.Command{
		Use:   "heartbeat <claim_id>",
		Short: "Renew a lease",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			opID, err := app.Flags.ResolveOpID(app.IDs)
			if err != nil {
				cli.WriteEnvelope(cmd.OutOrStdout(), cli.Envelope{Kind: "task.heartbeat", Err: err})
				os.Exit(cli.ExitCodeFor(err))
			}
			os.Exit(cli.Run(cmd.OutOrStdout(), "task.heartbeat", opID, func() (any, error) {
				h, err := openStateDB(app)
				if err != nil {
					return nil, err
				}
				defer h.Close()
				var res task.HeartbeatResult
				err = h.WithTx(cmd.Context(), func(tx *db.Tx) error {
					store := task.NewStore(tx, events.NewAppender(app.Clock), app.IDs, app.Clock)
					r, err := store.Heartbeat(task.HeartbeatInput{OpID: opID, ClaimID: args[0]})
					res = r
					return err
				})
				return res, err
			}))
			return nil
		},
	}
}

func newTaskReleaseCmd(app *App) *cobra.Command {
	return &cobra.Command{
		Use:   "release <claim_id>",
		Short: "Release a claim",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			opID, err := app.Flags.ResolveOpID(app.IDs)
			if err != nil {
				cli.WriteEnvelope(cmd.OutOrStdout(), cli.Envelope{Kind: "task.release", Err: err})
				os.Exit(cli.ExitCodeFor(err))
			}
			os.Exit(cli.Run(cmd.OutOrStdout(), "task.release", opID, func() (any, error) {
				h, err := openStateDB(app)
				if err != nil {
					return nil, err
				}
				defer h.Close()
				err = h.WithTx(cmd.Context(), func(tx *db.Tx) error {
					store := task.NewStore(tx, events.NewAppender(app.Clock), app.IDs, app.Clock)
					return store.Release(task.ReleaseInput{OpID: opID, ClaimID: args[0]})
				})
				return map[string]any{}, err
			}))
			return nil
		},
	}
}

func newTaskCompleteCmd(app *App) *cobra.Command {
	return &cobra.Command{
		Use:   "complete <claim_id>",
		Short: "Complete a task after all required gates fresh+pass",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			opID, err := app.Flags.ResolveOpID(app.IDs)
			if err != nil {
				cli.WriteEnvelope(cmd.OutOrStdout(), cli.Envelope{Kind: "task.complete", Err: err})
				os.Exit(cli.ExitCodeFor(err))
			}
			os.Exit(cli.Run(cmd.OutOrStdout(), "task.complete", opID, func() (any, error) {
				h, err := openStateDB(app)
				if err != nil {
					return nil, err
				}
				defer h.Close()
				var res task.CompleteResult
				err = h.WithTx(cmd.Context(), func(tx *db.Tx) error {
					store := task.NewStore(tx, events.NewAppender(app.Clock), app.IDs, app.Clock)
					r, err := store.Complete(task.CompleteInput{OpID: opID, ClaimID: args[0]})
					res = r
					return err
				})
				return res, err
			}))
			return nil
		},
	}
}
