use crate::task::Task;
use rusqlite::Connection;

pub fn init_db(project: &str) -> Result<Connection, Box<dyn std::error::Error>> {
    todo!()
}

pub fn create_task(conn: &Connection, task: &Task) -> Result<(), Box<dyn std::error::Error>> {
    todo!()
}

pub fn get_tasks(conn: &Connection) -> Result<Vec<Task>, Box<dyn std::error::Error>> {
    todo!()
}

pub fn update_task(conn: &Connection, task: &Task) -> Result<(), Box<dyn std::error::Error>> {
    todo!()
}

pub fn delete_task(conn: &Connection, id: &str) -> Result<(), Box<dyn std::error::Error>> {
    todo!()
}
