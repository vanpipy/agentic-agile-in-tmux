use crate::task::Task;
use rusqlite::Connection;

pub fn init_db(_project: &str) -> Result<Connection, Box<dyn std::error::Error>> {
    todo!()
}

pub fn create_task(_conn: &Connection, _task: &Task) -> Result<(), Box<dyn std::error::Error>> {
    todo!()
}

pub fn get_tasks(_conn: &Connection) -> Result<Vec<Task>, Box<dyn std::error::Error>> {
    todo!()
}

pub fn update_task(_conn: &Connection, _task: &Task) -> Result<(), Box<dyn std::error::Error>> {
    todo!()
}

pub fn delete_task(_conn: &Connection, _id: &str) -> Result<(), Box<dyn std::error::Error>> {
    todo!()
}
