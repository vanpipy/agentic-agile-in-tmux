// Package app implements the awp application orchestration.
//
// Phase 2: minimal TUI launcher + project/ticket/session CLI helpers.
package app

import (
		"fmt"
	"os"
	"path/filepath"

				"github.com/pi/awp/internal/config"
	"github.com/pi/awp/internal/project"
	)

// Run is the legacy Phase 0 entry point.
func Run(_ *config.Config, _ string, _ string) error {
	return fmt.Errorf("awp TUI is now launched via the awp CLI; run `awp` (no args)")
}

// CreateProject registers a new project for the given git repo path.
func CreateProject(_ *config.Config, name, repoPath string) error {
	if _, err := os.Stat(filepath.Join(repoPath, ".git")); err != nil {
		return fmt.Errorf("not a git repository: %s: %w", repoPath, err)
	}

	registry, err := project.LoadRegistry()
	if err != nil {
		return fmt.Errorf("failed to load project registry: %w", err)
	}

	if existing, _ := registry.FindByPath(repoPath); existing != nil {
		return fmt.Errorf("project already exists for %s: %s", repoPath, existing.Name)
	}

	p := project.NewProject(name, repoPath)
	if err := registry.Add(p); err != nil {
		return fmt.Errorf("failed to save project: %w", err)
	}

	fmt.Printf("Created project '%s' for %s\n", name, repoPath)
	fmt.Printf("Project ID: %s\n", p.ID)
	return nil
}

// ListProjects prints all registered projects.
func ListProjects() error {
	registry, err := project.LoadRegistry()
	if err != nil {
		return err
	}

	projects := registry.List()
	if len(projects) == 0 {
		fmt.Println("No projects found. Create one with: awp project new <name>")
		return nil
	}

	fmt.Println("Available projects:")
	fmt.Println()

	for _, p := range projects {
		tickets, err := project.LoadTicketStore(p)
		if err != nil {
			continue
		}

		total := tickets.Count()
		inProgress := tickets.CountByStatus("in_progress")

		fmt.Printf("  %s (%s)\n", p.Name, p.ID[:8])
		fmt.Printf("    Path: %s\n", p.RepoPath)
		fmt.Printf("    Tickets: %d total, %d in progress\n", total, inProgress)
		fmt.Println()
	}

	return nil
}

// DeleteProject removes a project by name or ID prefix.
func DeleteProject(nameOrID string) error {
	registry, err := project.LoadRegistry()
	if err != nil {
		return err
	}

	var target *project.Project
	for _, p := range registry.List() {
		if p.Name == nameOrID || p.ID == nameOrID || (len(p.ID) >= 8 && p.ID[:8] == nameOrID) {
			target = p
			break
		}
	}

	if target == nil {
		return fmt.Errorf("%s: %w", nameOrID, project.ErrProjectNotFound)
	}

	if err := registry.Delete(target.ID); err != nil {
		return fmt.Errorf("failed to delete project: %w", err)
	}

	fmt.Printf("Deleted project '%s' (%s)\n", target.Name, target.RepoPath)
	return nil
}

