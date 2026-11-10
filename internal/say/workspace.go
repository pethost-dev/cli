package say

import (
	"strings"

	pethost "github.com/pethost-dev/sdk/go"

	"github.com/pethost-dev/cli/internal/ui"
)

// WorkspaceName is a workspace as a sentence names it: `"Work" (k7f3q2xy)`.
func WorkspaceName(workspace *pethost.Workspace) string {
	return `"` + ui.Clean(workspace.Name) + `" (` + ui.Clean(workspace.WorkspaceID) + ")"
}

// WorkspaceSize is what a workspace's plan gives its machine: "2 CPU, 4 GB memory, 40 GB disk".
func WorkspaceSize(workspace *pethost.Workspace) string {
	return Cores(workspace.CPUTotalCores) + " CPU, " + ui.Size(workspace.MemoryTotalBytes) + " memory, " + ui.Size(workspace.DiskTotalBytes) + " disk"
}

// WorkspaceFree is what a workspace's projects leave of its memory and disk, by its machine's
// latest sample: "1.1 GB memory, 31 GB disk free"; what the API did not know is said so.
func WorkspaceFree(workspace *pethost.Workspace) string {
	switch {
	case workspace.Status == pethost.WorkspaceStatusBeingPrepared:
		return "being prepared"
	case workspace.MemoryFreeBytes == nil && workspace.DiskFreeBytes == nil:
		return "free: not known now"
	}
	return sizeOrUnknown(workspace.MemoryFreeBytes) + " memory, " + sizeOrUnknown(workspace.DiskFreeBytes) + " disk free"
}

func sizeOrUnknown(bytes *uint64) string {
	if bytes == nil {
		return "?"
	}
	return ui.Size(*bytes)
}

// WorkspaceProjects is how many projects a workspace runs: "4 projects"; "?" when not known.
func WorkspaceProjects(workspace *pethost.Workspace) string {
	if workspace.ProjectCount == nil {
		return "?"
	}
	return ui.Plural(uint64(*workspace.ProjectCount), "project")
}

// Workspace is a workspace in one line, as a picker shows it beside its name: "k7f3q2xy · 2
// CPU, 4 GB memory, 40 GB disk · 1.1 GB memory, 31 GB disk free · Europe · 4 projects".
func Workspace(workspace *pethost.Workspace) string {
	parts := []string{ui.Clean(workspace.WorkspaceID), WorkspaceSize(workspace), WorkspaceFree(workspace)}
	if workspace.Location != "" {
		parts = append(parts, ui.Clean(workspace.Location))
	}
	if workspace.ProjectCount != nil {
		parts = append(parts, WorkspaceProjects(workspace))
	}
	return strings.Join(parts, " · ")
}
