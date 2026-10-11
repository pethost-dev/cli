// Package say puts the API's values into a person's words, the same on every screen: a
// problem's line, a service's state, an operation's kind. The machine's own sentences are
// printed as it wrote them; only its enums and times are worded here.
package say

import (
	"strconv"
	"strings"

	pethost "github.com/pethost-dev/sdk/go"

	"github.com/pethost-dev/cli/internal/ui"
)

// Problem is a project's problem as one line, without its mark: the machine's sentence and
// since when, "worker keeps restarting (since 13:58)". The time stands apart from the sentence:
// the machine's own words may end with a "since" or a fact of their own, which the time would
// otherwise read as part of.
func Problem(stream *ui.Stream, problem pethost.ProjectProblem) string {
	line := ui.Clean(problem.ProblemMessage)
	if !problem.SinceTime.IsZero() {
		line += " (since " + stream.At(problem.SinceTime) + ")"
	}
	return line
}

// State is a service's state as a word: "running", "restarting", "not created".
func State(state pethost.ServiceState) string {
	return strings.ToLower(strings.ReplaceAll(state.String(), "_", " "))
}

// Runs says whether a state is one of a service that runs: running, with or without a
// healthcheck that passes.
func Runs(state pethost.ServiceState) bool {
	return state == pethost.ServiceStateRunning || state == pethost.ServiceStateHealthy
}

// Doing is an operation in progress as a word: "deploying", "backing up".
func Doing(kind pethost.OperationKind) string {
	switch kind {
	case pethost.OperationKindDeploy:
		return "deploying"
	case pethost.OperationKindRecreateService:
		return "recreating a service"
	case pethost.OperationKindBackup:
		return "backing up"
	case pethost.OperationKindRestore:
		return "restoring"
	case pethost.OperationKindDelete:
		return "deleting"
	}
	return strings.ToLower(kind.String())
}

// Operation names an operation's kind in a sentence: "a deploy", "a backup".
func Operation(kind pethost.OperationKind) string {
	switch kind {
	case pethost.OperationKindDeploy:
		return "a deploy"
	case pethost.OperationKindRecreateService:
		return "a service's recreation"
	case pethost.OperationKindBackup:
		return "a backup"
	case pethost.OperationKindRestore:
		return "a restore"
	case pethost.OperationKindDelete:
		return "its deletion"
	}
	return "an operation (" + strings.ToLower(kind.String()) + ")"
}

// Ended is how an operation ended as a word: "succeeded", "failed", "was cancelled".
func Ended(status pethost.OperationStatus) string {
	switch status {
	case pethost.OperationStatusSucceeded:
		return "succeeded"
	case pethost.OperationStatusFailed:
		return "failed"
	case pethost.OperationStatusCancelled:
		return "was cancelled"
	case pethost.OperationStatusInProgress:
		return "is running"
	}
	return strings.ToLower(status.String())
}

// Machine is the machine in a few words: "Starter · 2 CPU, 3 GB · sam.pethost.app".
func Machine(machine *pethost.Machine) string {
	words := ui.Clean(machine.Name) + " · " + Resources(machine)
	if machine.AppsDomain != "" {
		words += " · " + ui.Clean(machine.AppsDomain)
	}
	return words
}

// Resources is what the plan gives the machine: "2 CPU, 3 GB".
func Resources(machine *pethost.Machine) string {
	return Cores(machine.CPUTotalCores) + " CPU, " + ui.Size(machine.MemoryTotalBytes)
}

// Cores is a number of CPU cores: "2", "0.3".
func Cores(count float64) string {
	return strings.TrimSuffix(strconv.FormatFloat(count, 'f', 1, 64), ".0")
}
