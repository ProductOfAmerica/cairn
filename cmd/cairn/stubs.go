package main

import "github.com/spf13/cobra"

// Temporary stubs. Replaced by subsequent tasks.
func newTaskCmd(_ *App) *cobra.Command     { return &cobra.Command{Use: "task", Hidden: true} }
func newVerdictCmd(_ *App) *cobra.Command  { return &cobra.Command{Use: "verdict", Hidden: true} }
func newEvidenceCmd(_ *App) *cobra.Command { return &cobra.Command{Use: "evidence", Hidden: true} }
func newEventsCmd(_ *App) *cobra.Command   { return &cobra.Command{Use: "events", Hidden: true} }
