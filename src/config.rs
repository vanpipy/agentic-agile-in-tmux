use serde::{Deserialize, Serialize};
use std::path::PathBuf;

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct BoardConfig {
    pub db_path: PathBuf,
    pub projects_dir: PathBuf,
    pub default_priority: u8,
}

pub fn load_config(project: &str) -> Result<BoardConfig, Box<dyn std::error::Error>> {
    todo!()
}

pub fn default_config() -> BoardConfig {
    todo!()
}
