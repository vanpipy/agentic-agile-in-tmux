use directories::ProjectDirs;
use serde::{Deserialize, Serialize};
use std::path::PathBuf;

#[derive(Debug, Clone, Serialize, Deserialize, PartialEq)]
#[serde(untagged)]
pub enum ColumnLimit {
    Unlimited,
    Limited(u8),
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct ColumnConfig {
    pub name: String,
    pub limit: ColumnLimit,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct WorktreeConfig {
    pub base_path: String,
    pub branch_prefix: String,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct BoardConfig {
    pub columns: Vec<ColumnConfig>,
    pub worktree: WorktreeConfig,
}

pub fn load_config(project: &str) -> Result<BoardConfig, Box<dyn std::error::Error>> {
    let config_path = get_config_path(project)?;

    if !config_path.exists() {
        return Ok(default_config());
    }

    let content = std::fs::read_to_string(&config_path)?;
    let config: BoardConfig = serde_yaml::from_str(&content)
        .map_err(|e| format!("Invalid YAML in {}: {}", config_path.display(), e))?;

    Ok(config)
}

pub fn default_config() -> BoardConfig {
    BoardConfig {
        columns: vec![
            ColumnConfig {
                name: "To Do".to_string(),
                limit: ColumnLimit::Limited(5),
            },
            ColumnConfig {
                name: "In Progress".to_string(),
                limit: ColumnLimit::Limited(3),
            },
            ColumnConfig {
                name: "Done".to_string(),
                limit: ColumnLimit::Unlimited,
            },
        ],
        worktree: WorktreeConfig {
            base_path: "{repo}/.worktrees".to_string(),
            branch_prefix: "task/".to_string(),
        },
    }
}

fn get_config_path(project: &str) -> Result<PathBuf, Box<dyn std::error::Error>> {
    let proj_dirs = ProjectDirs::from("com", "ait", "agentic-agile-tui")
        .ok_or("Could not determine config directory")?;

    let config_dir = proj_dirs.config_dir().join(project);
    Ok(config_dir.join("board.yaml"))
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_default_config() {
        let config = default_config();
        assert_eq!(config.columns.len(), 3);
        assert_eq!(config.columns[0].name, "To Do");
        assert_eq!(config.columns[0].limit, ColumnLimit::Limited(5));
        assert_eq!(config.columns[1].name, "In Progress");
        assert_eq!(config.columns[1].limit, ColumnLimit::Limited(3));
        assert_eq!(config.columns[2].name, "Done");
        assert_eq!(config.columns[2].limit, ColumnLimit::Unlimited);
    }

    #[test]
    fn test_parse_valid_yaml() {
        let yaml = r#"
columns:
  - name: Backlog
    limit: 10
  - name: In Progress
    limit: 2
  - name: Done
    limit: unlimited

worktree:
  base_path: "{repo}/.worktrees"
  branch_prefix: "feature/"
"#;
        let config: BoardConfig = serde_yaml::from_str(yaml).unwrap();
        assert_eq!(config.columns.len(), 3);
        assert_eq!(config.columns[0].name, "Backlog");
        assert_eq!(config.columns[0].limit, ColumnLimit::Limited(10));
        assert_eq!(config.columns[2].limit, ColumnLimit::Unlimited);
        assert_eq!(config.worktree.branch_prefix, "feature/");
    }

    #[test]
    fn test_parse_invalid_yaml() {
        let yaml = r#"
columns:
  - name: Test
    limit: [invalid
"#;
        let result: Result<BoardConfig, _> = serde_yaml::from_str(yaml);
        assert!(result.is_err());
    }

    #[test]
    fn test_column_limit_deserialization() {
        let limited: ColumnLimit = serde_yaml::from_str("5").unwrap();
        assert_eq!(limited, ColumnLimit::Limited(5));

        let unlimited: ColumnLimit = serde_yaml::from_str("unlimited").unwrap();
        assert_eq!(unlimited, ColumnLimit::Unlimited);
    }

    #[test]
    fn test_load_config_missing_file() {
        let result = load_config("nonexistent_project_12345");
        assert!(result.is_ok());
        let config = result.unwrap();
        assert_eq!(config.columns.len(), 3);
    }
}
