use chrono::{DateTime, Utc};
use serde::{Deserialize, Serialize};
use uuid::Uuid;

#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
pub enum Status {
    Open,
    InProgress,
    Blocked,
    Done,
}

impl std::fmt::Display for Status {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        match self {
            Status::Open => write!(f, "Open"),
            Status::InProgress => write!(f, "InProgress"),
            Status::Blocked => write!(f, "Blocked"),
            Status::Done => write!(f, "Done"),
        }
    }
}

impl std::str::FromStr for Status {
    type Err = String;
    fn from_str(s: &str) -> Result<Self, Self::Err> {
        match s {
            "Open" => Ok(Status::Open),
            "InProgress" => Ok(Status::InProgress),
            "Blocked" => Ok(Status::Blocked),
            "Done" => Ok(Status::Done),
            _ => Err(format!("Invalid status: {}", s)),
        }
    }
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct Task {
    pub id: String,
    pub title: String,
    pub branch: String,
    pub status: Status,
    pub column: String,
    pub created_at: DateTime<Utc>,
    pub updated_at: DateTime<Utc>,
}

impl Task {
    pub fn new(title: &str, branch: &str, column: &str) -> Self {
        let now = Utc::now();
        Task {
            id: Uuid::new_v4().to_string(),
            title: title.to_string(),
            branch: branch.to_string(),
            status: Status::Open,
            column: column.to_string(),
            created_at: now,
            updated_at: now,
        }
    }
}

pub fn validate_task(task: &Task) -> Result<(), String> {
    if task.title.is_empty() {
        return Err("Title cannot be empty".to_string());
    }
    if task.title.len() > 100 {
        return Err("Title cannot exceed 100 characters".to_string());
    }
    if task.branch.is_empty() {
        return Err("Branch cannot be empty".to_string());
    }
    if !task
        .branch
        .chars()
        .all(|c| c.is_ascii_lowercase() || c.is_ascii_digit() || c == '-')
    {
        return Err(
            "Branch must contain only lowercase alphanumeric characters and hyphens".to_string(),
        );
    }
    if !task.branch.starts_with(|c: char| c.is_ascii_alphabetic()) {
        return Err("Branch must start with a letter".to_string());
    }
    if task.column.is_empty() {
        return Err("Column cannot be empty".to_string());
    }
    Ok(())
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_task_new() {
        let task = Task::new("Test Task", "feature-test", "todo");
        assert!(!task.id.is_empty());
        assert_eq!(task.title, "Test Task");
        assert_eq!(task.branch, "feature-test");
        assert_eq!(task.status, Status::Open);
        assert_eq!(task.column, "todo");
    }

    #[test]
    fn test_validate_task_valid() {
        let task = Task::new("Valid Task", "feature-abc", "todo");
        assert!(validate_task(&task).is_ok());
    }

    #[test]
    fn test_validate_task_empty_title() {
        let task = Task::new("", "feature-test", "todo");
        assert!(validate_task(&task).is_err());
        assert_eq!(validate_task(&task).unwrap_err(), "Title cannot be empty");
    }

    #[test]
    fn test_validate_task_title_too_long() {
        let task = Task::new(&"a".repeat(101), "feature-test", "todo");
        assert!(validate_task(&task).is_err());
        assert_eq!(
            validate_task(&task).unwrap_err(),
            "Title cannot exceed 100 characters"
        );
    }

    #[test]
    fn test_validate_task_empty_branch() {
        let task = Task::new("Task", "", "todo");
        assert!(validate_task(&task).is_err());
        assert_eq!(validate_task(&task).unwrap_err(), "Branch cannot be empty");
    }

    #[test]
    fn test_validate_task_branch_with_spaces() {
        let task = Task::new("Task", "feature test", "todo");
        assert!(validate_task(&task).is_err());
        assert_eq!(
            validate_task(&task).unwrap_err(),
            "Branch must contain only lowercase alphanumeric characters and hyphens"
        );
    }

    #[test]
    fn test_validate_task_branch_uppercase() {
        let task = Task::new("Task", "Feature-Test", "todo");
        assert!(validate_task(&task).is_err());
        assert_eq!(
            validate_task(&task).unwrap_err(),
            "Branch must contain only lowercase alphanumeric characters and hyphens"
        );
    }

    #[test]
    fn test_validate_task_branch_starts_with_number() {
        let task = Task::new("Task", "123-feature", "todo");
        assert!(validate_task(&task).is_err());
        assert_eq!(
            validate_task(&task).unwrap_err(),
            "Branch must start with a letter"
        );
    }

    #[test]
    fn test_validate_task_empty_column() {
        let task = Task::new("Task", "feature-test", "");
        assert!(validate_task(&task).is_err());
        assert_eq!(validate_task(&task).unwrap_err(), "Column cannot be empty");
    }

    #[test]
    fn test_status_to_string() {
        assert_eq!(Status::Open.to_string(), "Open");
        assert_eq!(Status::InProgress.to_string(), "InProgress");
        assert_eq!(Status::Blocked.to_string(), "Blocked");
        assert_eq!(Status::Done.to_string(), "Done");
    }

    #[test]
    fn test_status_from_str() {
        assert_eq!("Open".parse::<Status>().unwrap(), Status::Open);
        assert_eq!("InProgress".parse::<Status>().unwrap(), Status::InProgress);
        assert_eq!("Blocked".parse::<Status>().unwrap(), Status::Blocked);
        assert_eq!("Done".parse::<Status>().unwrap(), Status::Done);
        assert!("Invalid".parse::<Status>().is_err());
    }
}
