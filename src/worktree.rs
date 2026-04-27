use crate::task::Task;
use std::path::{Path, PathBuf};

pub fn create_worktree(
    repo_path: &Path,
    task: &Task,
) -> Result<PathBuf, Box<dyn std::error::Error>> {
    todo!()
}

pub fn worktree_path(repo_path: &Path, task: &Task) -> PathBuf {
    todo!()
}

pub fn worktree_exists(repo_path: &Path, task: &Task) -> bool {
    todo!()
}
