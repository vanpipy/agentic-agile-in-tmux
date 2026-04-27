use clap::Parser;
use ratatui::crossterm::event::{Event, KeyCode, KeyEventKind};
use ratatui::layout::{Constraint, Direction, Layout, Margin, Rect};
use ratatui::prelude::*;
use ratatui::widgets::{Block, Borders, Clear, Paragraph};
use ratatui::Terminal;
use rusqlite::Connection;
use std::path::PathBuf;
use tracing::{error, info, Level};
use tracing_subscriber::FmtSubscriber;

mod config;
mod db;
mod task;
mod tmux;
mod ui;
mod worktree;

use config::{load_config, BoardConfig};
use task::{Status, Task};
use ui::{BoardWidget, CreateDialog, StatusBar};

#[derive(Parser, Debug)]
#[command(name = "agentic-agile-tui")]
#[command(about = "Terminal UI for agentic agile project management")]
struct Args {
    #[arg(short, long, help = "Project name")]
    project: Option<String>,

    #[arg(short, long, help = "Show help")]
    help: bool,
}

enum Command {
    CreateTask {
        title: String,
        branch: String,
        column: String,
    },
    MoveTask {
        id: String,
        to_column: String,
    },
    DeleteTask {
        id: String,
    },
    AttachTmux {
        id: String,
    },
    EditTask {
        id: String,
        field: String,
        value: String,
    },
    MoveSelectionDown,
    MoveSelectionUp,
    MoveSelectionLeft,
    MoveSelectionRight,
    SelectTask,
    StartTask,
    BlockTask,
    CompleteTask,
    ToggleCreateDialog,
    ConfirmCreate,
    CancelCreate,
}

struct AppState {
    tasks: Vec<Task>,
    board: BoardWidget,
    show_create_dialog: bool,
    create_dialog: CreateDialog,
    status_bar: StatusBar,
    config: BoardConfig,
    error_message: Option<String>,
}

impl AppState {
    fn new(config: BoardConfig, project_name: String) -> Self {
        AppState {
            tasks: Vec::new(),
            board: BoardWidget::new(config.clone()),
            show_create_dialog: false,
            create_dialog: CreateDialog::new(),
            status_bar: StatusBar::new(project_name),
            config,
            error_message: None,
        }
    }

    fn tasks_in_column(&self, column: &str) -> Vec<&Task> {
        self.tasks.iter().filter(|t| t.column == column).collect()
    }

    fn get_selected_task_id(&self) -> Option<String> {
        let col_tasks = self
            .board
            .tasks_in_column(&self.tasks, self.board.selected_column());
        let idx = self.board.selected_task()?;
        col_tasks.get(idx).map(|t| t.id.clone())
    }

    fn execute_command(
        &mut self,
        cmd: Command,
        conn: &Connection,
        project: &str,
    ) -> Result<(), String> {
        match cmd {
            Command::CreateTask {
                title,
                branch,
                column,
            } => {
                let new_task = Task::new(&title, &branch, &column);
                task::validate_task(&new_task).map_err(|e| e.to_string())?;
                db::create_task(conn, &new_task).map_err(|e| e.to_string())?;
                self.tasks.push(new_task);
                let msg = format!("Created task: {}", title);
                self.status_bar.set_message(msg);
                info!(title = %title, branch = %branch, column = %column, "Task created");
                Ok(())
            }
            Command::DeleteTask { id } => {
                db::delete_task(conn, &id).map_err(|e| e.to_string())?;
                self.tasks.retain(|t| t.id != id);
                self.status_bar.set_message(format!("Deleted task"));
                info!(id = %id, "Task deleted");
                Ok(())
            }
            Command::MoveTask { id, to_column } => {
                if let Some(task) = self.tasks.iter_mut().find(|t| t.id == id) {
                    task.column = to_column.clone();
                    task.updated_at = chrono::Utc::now();
                    db::update_task(conn, task).map_err(|e| e.to_string())?;
                    self.status_bar
                        .set_message(format!("Moved task to {}", to_column));
                    info!(id = %id, column = %to_column, "Task moved");
                }
                Ok(())
            }
            Command::AttachTmux { id } => {
                if let Some(task) = self.tasks.iter().find(|t| t.id == id) {
                    let session_name = format!("ait-{}-{}", project, task.branch);
                    tmux::create_session(project, task).map_err(|e| e.to_string())?;
                    tmux::attach_session(&session_name).map_err(|e| e.to_string())?;
                    self.status_bar
                        .set_message(format!("Attached to {}", session_name));
                    info!(id = %id, session = %session_name, "Attached to tmux session");
                }
                Ok(())
            }
            Command::EditTask { id, field, value } => {
                if let Some(task) = self.tasks.iter_mut().find(|t| t.id == id) {
                    match field.as_str() {
                        "title" => task.title = value,
                        "branch" => task.branch = value,
                        "status" => {
                            task.status = value.parse().map_err(|e: String| e)?;
                        }
                        _ => return Err(format!("Unknown field: {}", field)),
                    }
                    task.updated_at = chrono::Utc::now();
                    db::update_task(conn, task).map_err(|e| e.to_string())?;
                    self.status_bar
                        .set_message(format!("Edited {} field", field));
                    info!(id = %id, field = %field, "Task edited");
                }
                Ok(())
            }
            Command::MoveSelectionDown => {
                self.board.move_down(&self.tasks);
                Ok(())
            }
            Command::MoveSelectionUp => {
                self.board.move_up();
                Ok(())
            }
            Command::MoveSelectionLeft => {
                self.board.move_left();
                Ok(())
            }
            Command::MoveSelectionRight => {
                self.board.move_right();
                Ok(())
            }
            Command::SelectTask => Ok(()),
            Command::StartTask => {
                if let Some(task_id) = self.get_selected_task_id() {
                    if let Some(task) = self.tasks.iter_mut().find(|t| t.id == task_id) {
                        task.status = Status::InProgress;
                        task.updated_at = chrono::Utc::now();
                        db::update_task(conn, task).map_err(|e| e.to_string())?;
                        self.status_bar.set_message("Task started".to_string());
                        info!(id = %task_id, "Task started");
                    }
                }
                Ok(())
            }
            Command::BlockTask => {
                if let Some(task_id) = self.get_selected_task_id() {
                    if let Some(task) = self.tasks.iter_mut().find(|t| t.id == task_id) {
                        task.status = Status::Blocked;
                        task.updated_at = chrono::Utc::now();
                        db::update_task(conn, task).map_err(|e| e.to_string())?;
                        self.status_bar.set_message("Task blocked".to_string());
                        info!(id = %task_id, "Task blocked");
                    }
                }
                Ok(())
            }
            Command::CompleteTask => {
                if let Some(task_id) = self.get_selected_task_id() {
                    if let Some(task) = self.tasks.iter_mut().find(|t| t.id == task_id) {
                        task.status = Status::Done;
                        task.updated_at = chrono::Utc::now();
                        db::update_task(conn, task).map_err(|e| e.to_string())?;
                        self.status_bar.set_message("Task completed".to_string());
                        info!(id = %task_id, "Task completed");
                    }
                }
                Ok(())
            }
            Command::ToggleCreateDialog => {
                self.show_create_dialog = !self.show_create_dialog;
                if !self.show_create_dialog {
                    self.create_dialog = CreateDialog::new();
                }
                Ok(())
            }
            Command::ConfirmCreate => {
                if !self.create_dialog.title.is_empty() && !self.create_dialog.branch.is_empty() {
                    let column = self
                        .board
                        .column_names()
                        .get(self.create_dialog.column_index)
                        .cloned()
                        .unwrap_or_else(|| "To Do".to_string());
                    return self.execute_command(
                        Command::CreateTask {
                            title: self.create_dialog.title.clone(),
                            branch: self.create_dialog.branch.clone(),
                            column,
                        },
                        conn,
                        project,
                    );
                }
                self.show_create_dialog = false;
                self.create_dialog = CreateDialog::new();
                Ok(())
            }
            Command::CancelCreate => {
                self.show_create_dialog = false;
                self.create_dialog = CreateDialog::new();
                Ok(())
            }
        }
    }
}

fn get_log_dir(project: &str) -> Result<PathBuf, Box<dyn std::error::Error>> {
    let proj_dirs = directories::ProjectDirs::from("com", "ait", "agentic-agile-tui")
        .ok_or("Could not determine data directory")?;
    let log_dir = proj_dirs.data_dir().join(project).join("logs");
    Ok(log_dir)
}

fn setup_logging(project: &str) -> Result<(), Box<dyn std::error::Error>> {
    let log_dir = get_log_dir(project)?;
    std::fs::create_dir_all(&log_dir)?;
    let log_file_path = log_dir.join("app.log");
    let log_file_path_for_info = log_file_path.clone();

    FmtSubscriber::builder()
        .with_max_level(Level::INFO)
        .with_writer(move || {
            let file = std::fs::OpenOptions::new()
                .create(true)
                .append(true)
                .open(&log_file_path)
                .expect("Failed to open log file");
            file
        })
        .with_ansi(false)
        .try_init()
        .map_err(|_| "Failed to set tracing subscriber")?;

    info!(project = %project, log_path = %log_file_path_for_info.display(), "Logging initialized");
    Ok(())
}

fn render_ui(f: &mut Frame<'_>, state: &AppState) {
    let areas = Layout::default()
        .direction(Direction::Vertical)
        .constraints(
            [
                Constraint::Length(3),
                Constraint::Min(0),
                Constraint::Length(3),
            ]
            .as_ref(),
        )
        .split(f.area());

    let header_area = areas[0];
    let body_area = areas[1];
    let status_area = areas[2];

    let title = Text::raw(format!(
        " Agentic Agile TUI - Project: {} ",
        state
            .config
            .columns
            .first()
            .map(|_| "project")
            .unwrap_or("")
    ));
    let title_widget = Paragraph::new(title)
        .style(Style::default().fg(Color::White).bg(Color::Blue))
        .block(Block::default().borders(Borders::ALL).title("Header"));
    f.render_widget(title_widget, header_area);

    if state.show_create_dialog {
        state
            .create_dialog
            .render(f, body_area, &state.board.column_names());
    } else {
        state.board.render(f, body_area, &state.tasks);
    }

    state.status_bar.render(f, status_area);

    if let Some(ref err) = state.error_message {
        let error_area = Rect {
            x: 0,
            y: f.area().height.saturating_sub(3),
            width: f.area().width,
            height: 3,
        };
        let error_widget = Paragraph::new(Text::raw(format!("Error: {}", err)))
            .style(Style::default().fg(Color::Red))
            .block(Block::default().borders(Borders::ALL));
        f.render_widget(error_widget, error_area);
    }
}

fn run_event_loop(
    mut terminal: Terminal<CrosstermBackend<std::io::Stdout>>,
    conn: Connection,
    project: String,
    config: BoardConfig,
) -> Result<(), Box<dyn std::error::Error>> {
    let mut state = AppState::new(config, project.clone());
    state.tasks = db::get_tasks(&conn).unwrap_or_default();

    loop {
        terminal.draw(|f| render_ui(f, &state))?;

        match ratatui::crossterm::event::poll(std::time::Duration::from_millis(100)) {
            Ok(true) => {}
            Ok(false) => continue,
            Err(e) => {
                error!(error = %e, "Event poll error");
                continue;
            }
        }

        let event = ratatui::crossterm::event::read()?;

        if let Event::Key(key) = event {
            if key.kind != KeyEventKind::Press {
                continue;
            }

            if state.show_create_dialog {
                if let Some(cmd) = state.create_dialog.handle_key(key) {
                    match cmd {
                        ui::DialogCommand::Confirm => {
                            if let Err(e) =
                                state.execute_command(Command::ConfirmCreate, &conn, &project)
                            {
                                state.error_message = Some(e);
                            } else {
                                state.error_message = None;
                                state.show_create_dialog = false;
                            }
                        }
                        ui::DialogCommand::Cancel => {
                            state
                                .execute_command(Command::CancelCreate, &conn, &project)
                                .ok();
                            state.show_create_dialog = false;
                        }
                    }
                }
                continue;
            }

            let cmd = match key.code {
                KeyCode::Char('q') => break,
                KeyCode::Char('n') => Some(Command::ToggleCreateDialog),
                KeyCode::Char('j') => Some(Command::MoveSelectionDown),
                KeyCode::Char('k') => Some(Command::MoveSelectionUp),
                KeyCode::Char('h') => Some(Command::MoveSelectionLeft),
                KeyCode::Char('l') => Some(Command::MoveSelectionRight),
                KeyCode::Char('d') => Some(Command::DeleteTask {
                    id: state.get_selected_task_id().unwrap_or_default(),
                }),
                KeyCode::Char('a') => Some(Command::AttachTmux {
                    id: state.get_selected_task_id().unwrap_or_default(),
                }),
                KeyCode::Char('s') => Some(Command::StartTask),
                KeyCode::Char('b') => Some(Command::BlockTask),
                KeyCode::Char('c') => Some(Command::CompleteTask),
                KeyCode::Enter => Some(Command::ConfirmCreate),
                KeyCode::Esc => Some(Command::CancelCreate),
                KeyCode::Left => Some(Command::MoveSelectionLeft),
                KeyCode::Right => Some(Command::MoveSelectionRight),
                _ => None,
            };

            if let Some(command) = cmd {
                if let Err(e) = state.execute_command(command, &conn, &project) {
                    state.error_message = Some(e);
                } else {
                    state.error_message = None;
                }
            }
        }
    }

    terminal.clear()?;
    info!("Application terminated normally");
    Ok(())
}

fn main() -> Result<(), Box<dyn std::error::Error>> {
    let args = Args::parse();

    if args.help {
        println!("agentic-agile-tui - Terminal UI for agentic agile project management");
        println!("Usage: agentic-agile-tui [--project NAME]");
        println!("  --project NAME    Project name (required)");
        println!("  --help            Show this help message");
        return Ok(());
    }

    let project = args
        .project
        .ok_or("Project name is required (--project NAME)")?;

    setup_logging(&project)?;

    info!(project = %project, "Starting application");

    let config = load_config(&project)?;
    info!(project = %project, "Config loaded");

    let conn = db::init_db(&project)?;
    info!(project = %project, "Database initialized");

    let backend = CrosstermBackend::new(std::io::stdout());
    let terminal = Terminal::new(backend)?;

    info!(project = %project, "Terminal initialized, entering event loop");

    if let Err(e) = run_event_loop(terminal, conn, project, config) {
        error!(error = %e, "Event loop terminated with error");
        eprintln!("Error: {}", e);
        std::process::exit(1);
    }

    Ok(())
}
