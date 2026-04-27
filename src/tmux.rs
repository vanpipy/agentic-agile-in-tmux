use crate::task::Task;

pub fn create_session(project: &str, task: &Task) -> Result<(), Box<dyn std::error::Error>> {
    todo!()
}

pub fn attach_session(session_name: &str) -> Result<(), Box<dyn std::error::Error>> {
    todo!()
}

pub fn destroy_session(session_name: &str) -> Result<(), Box<dyn std::error::Error>> {
    todo!()
}

pub fn session_exists(session_name: &str) -> bool {
    todo!()
}
