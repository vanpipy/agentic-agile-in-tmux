use std::process::Command;
use tracing::{error, info, warn};

use crate::task::Task;

#[derive(Debug)]
pub enum TmuxError {
    NotInstalled,
    CommandFailed(String),
    SessionExists,
    SessionNotFound,
}

impl std::fmt::Display for TmuxError {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        match self {
            TmuxError::NotInstalled => write!(f, "tmux is not installed"),
            TmuxError::CommandFailed(msg) => write!(f, "tmux command failed: {}", msg),
            TmuxError::SessionExists => write!(f, "session already exists"),
            TmuxError::SessionNotFound => write!(f, "session not found"),
        }
    }
}

impl std::error::Error for TmuxError {}

fn check_tmux_installed() -> Result<(), TmuxError> {
    let output = Command::new("tmux")
        .arg("-V")
        .output()
        .map_err(|_| TmuxError::NotInstalled)?;

    if !output.status.success() {
        return Err(TmuxError::NotInstalled);
    }
    Ok(())
}

fn session_name(project: &str, task: &Task) -> String {
    format!("ait-{}-{}", project, task.branch)
}

#[allow(dead_code)]
fn run_tmux_command(args: &[&str]) -> Result<String, TmuxError> {
    let output = Command::new("tmux")
        .args(args)
        .output()
        .map_err(|e| TmuxError::CommandFailed(e.to_string()))?;

    if !output.status.success() {
        let stderr = String::from_utf8_lossy(&output.stderr);
        return Err(TmuxError::CommandFailed(stderr.to_string()));
    }

    Ok(String::from_utf8_lossy(&output.stdout).to_string())
}

pub fn create_session(project: &str, task: &Task) -> Result<(), Box<dyn std::error::Error>> {
    check_tmux_installed()?;

    let name = session_name(project, task);
    let worktree_path = "/home/leroy/Project/agentic-agile-in-tmux/.worktrees/impl".to_string();

    if session_exists(&name) {
        warn!("Session {} already exists", name);
        return Err(Box::new(TmuxError::SessionExists));
    }

    info!("Creating tmux session: {}", name);

    let output = Command::new("tmux")
        .args(["new-session", "-d", "-s", &name, "-c", &worktree_path])
        .output()
        .map_err(|e| TmuxError::CommandFailed(e.to_string()))?;

    if !output.status.success() {
        let stderr = String::from_utf8_lossy(&output.stderr);
        error!("Failed to create session {}: {}", name, stderr);
        return Err(Box::new(TmuxError::CommandFailed(stderr.to_string())));
    }

    info!("Created tmux session: {}", name);
    Ok(())
}

pub fn attach_session(session_name: &str) -> Result<(), Box<dyn std::error::Error>> {
    check_tmux_installed()?;

    if !session_exists(session_name) {
        return Err(Box::new(TmuxError::SessionNotFound));
    }

    info!("Attaching to tmux session: {}", session_name);

    Command::new("tmux")
        .args(["detach-client", "-E"])
        .spawn()
        .map_err(|e| TmuxError::CommandFailed(e.to_string()))?;

    Command::new("tmux")
        .args(["attach-session", "-t", session_name])
        .spawn()
        .map_err(|e| TmuxError::CommandFailed(e.to_string()))?;

    Ok(())
}

#[allow(dead_code)]
pub fn destroy_session(session_name: &str) -> Result<(), Box<dyn std::error::Error>> {
    check_tmux_installed()?;

    if !session_exists(session_name) {
        warn!(
            "Session {} does not exist, nothing to destroy",
            session_name
        );
        return Ok(());
    }

    info!("Destroying tmux session: {}", session_name);

    run_tmux_command(&["kill-session", "-t", session_name])?;

    info!("Destroyed tmux session: {}", session_name);
    Ok(())
}

pub fn session_exists(session_name: &str) -> bool {
    if let Err(e) = check_tmux_installed() {
        warn!("Cannot check session existence: {}", e);
        return false;
    }

    match Command::new("tmux")
        .args(["has-session", "-t", session_name])
        .output()
    {
        Ok(output) => output.status.success(),
        Err(e) => {
            warn!("Failed to check session existence: {}", e);
            false
        }
    }
}

#[allow(dead_code)]
pub fn worktree_path(_project: &str, task: &Task) -> String {
    format!(
        "/home/leroy/Project/agentic-agile-in-tmux/.worktrees/{}",
        task.branch
    )
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_session_name_format() {
        let task = Task::new("Test Task", "feature-test", "todo");
        let name = session_name("myproject", &task);
        assert_eq!(name, "ait-myproject-feature-test");
    }

    #[test]
    fn test_worktree_path() {
        let task = Task::new("Test Task", "feature-test", "todo");
        let path = worktree_path("myproject", &task);
        assert_eq!(
            path,
            "/home/leroy/Project/agentic-agile-in-tmux/.worktrees/feature-test"
        );
    }

    #[test]
    fn test_tmux_error_display() {
        assert_eq!(TmuxError::NotInstalled.to_string(), "tmux is not installed");
        assert_eq!(
            TmuxError::CommandFailed("test".to_string()).to_string(),
            "tmux command failed: test"
        );
        assert_eq!(
            TmuxError::SessionExists.to_string(),
            "session already exists"
        );
        assert_eq!(TmuxError::SessionNotFound.to_string(), "session not found");
    }
}
