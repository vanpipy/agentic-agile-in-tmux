use crate::task::Task;
use std::path::{Path, PathBuf};
use std::process::Command;
use tracing::{debug, error, info};

#[allow(dead_code)]
pub fn create_worktree(
    repo_path: &Path,
    task: &Task,
) -> Result<PathBuf, Box<dyn std::error::Error + Send + Sync>> {
    let worktree_path = worktree_path(repo_path, task);
    let branch = &task.branch;

    info!(branch = %branch, path = %worktree_path.display(), "Creating worktree");

    let output = Command::new("git")
        .args([
            "worktree",
            "add",
            worktree_path.to_str().unwrap(),
            "-b",
            branch,
        ])
        .current_dir(repo_path)
        .output()?;

    if !output.status.success() {
        let stderr = String::from_utf8_lossy(&output.stderr);

        if stderr.contains("already exists") {
            error!("Branch '{}' already exists", branch);
            return Err(format!("Branch '{}' already exists", branch).into());
        }

        if stderr.contains("worktree") && stderr.contains("already exists") {
            error!("Worktree path '{}' already exists", worktree_path.display());
            return Err(
                format!("Worktree path '{}' already exists", worktree_path.display()).into(),
            );
        }

        error!(stderr = %stderr, "git worktree add failed");
        return Err(format!("git worktree add failed: {}", stderr).into());
    }

    debug!("git worktree add completed successfully");
    Ok(worktree_path)
}

#[allow(dead_code)]
pub fn worktree_path(repo_path: &Path, task: &Task) -> PathBuf {
    let worktrees_dir = repo_path.join(".worktrees").join(&task.branch);
    debug!(
        repo = %repo_path.display(),
        branch = %task.branch,
        path = %worktrees_dir.display(),
        "Computed worktree path"
    );
    worktrees_dir
}

#[allow(dead_code)]
pub fn worktree_exists(repo_path: &Path, task: &Task) -> bool {
    let worktree_path = worktree_path(repo_path, task);

    debug!(path = %worktree_path.display(), "Checking if worktree exists");

    let output = match Command::new("git")
        .args(["worktree", "list", "--porcelain"])
        .current_dir(repo_path)
        .output()
    {
        Ok(o) => o,
        Err(e) => {
            error!(error = %e, "Failed to execute git worktree list");
            return false;
        }
    };

    if !output.status.success() {
        debug!("git worktree list returned non-zero status");
        return false;
    }

    let stdout = String::from_utf8_lossy(&output.stdout);
    let target_path = worktree_path.to_string_lossy();

    for line in stdout.lines() {
        if line.starts_with("path ") {
            let path = line.trim_start_matches("path ").trim();
            if path == target_path {
                debug!("Found matching worktree at {}", path);
                return true;
            }
        }
    }

    debug!("Worktree not found at expected path");
    false
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::task::{Status, Task};
    use chrono::Utc;
    #[allow(unused_imports)]
    use std::fs;
    use tempfile::TempDir;

    fn make_test_task(branch: &str) -> Task {
        Task {
            id: "test-id".to_string(),
            title: "Test Task".to_string(),
            branch: branch.to_string(),
            status: Status::Open,
            column: "todo".to_string(),
            created_at: Utc::now(),
            updated_at: Utc::now(),
        }
    }

    #[test]
    fn test_worktree_path() {
        let temp = TempDir::new().unwrap();
        let task = make_test_task("feature-test");
        let path = worktree_path(temp.path(), &task);
        assert_eq!(path, temp.path().join(".worktrees").join("feature-test"));
    }

    #[test]
    fn test_worktree_path_special_chars() {
        let temp = TempDir::new().unwrap();
        let task = make_test_task("feature-my-task");
        let path = worktree_path(temp.path(), &task);
        assert_eq!(path, temp.path().join(".worktrees").join("feature-my-task"));
    }

    #[test]
    fn test_worktree_exists_no_worktrees() {
        let temp = TempDir::new().unwrap();
        let task = make_test_task("feature-test");

        let output = Command::new("git")
            .args(["init"])
            .current_dir(temp.path())
            .output()
            .unwrap();
        assert!(output.status.success());

        assert!(!worktree_exists(temp.path(), &task));
    }

    #[test]
    fn test_worktree_path_branch_name_used() {
        let temp = TempDir::new().unwrap();
        let task = Task {
            id: "id".to_string(),
            title: "My Task".to_string(),
            branch: "feature-my-task".to_string(),
            status: Status::Open,
            column: "col".to_string(),
            created_at: Utc::now(),
            updated_at: Utc::now(),
        };
        let path = worktree_path(temp.path(), &task);
        assert!(path.to_string_lossy().ends_with("feature-my-task"));
    }
}
