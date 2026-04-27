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
mod worktree;

use config::{load_config, BoardConfig};
use task::{Status, Task};

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
    selected_task: Option<usize>,
    selected_column: usize,
    show_create_dialog: bool,
    create_title: String,
    create_branch: String,
    config: BoardConfig,
    error_message: Option<String>,
}

impl AppState {
    fn new(config: BoardConfig) -> Self {
        AppState {
            tasks: Vec::new(),
            selected_task: None,
            selected_column: 0,
            show_create_dialog: false,
            create_title: String::new(),
            create_branch: String::new(),
            config,
            error_message: None,
        }
    }

    fn tasks_in_column(&self, column: &str) -> Vec<&Task> {
        self.tasks.iter().filter(|t| t.column == column).collect()
    }

    fn get_selected_task_id(&self) -> Option<String> {
        let col_name = self.config.columns.get(self.selected_column)?.name.clone();
        let col_tasks: Vec<&Task> = self.tasks.iter().filter(|t| t.column == col_name).collect();
        let idx = self.selected_task?;
        col_tasks.get(idx).map(|t| t.id.clone())
    }

    fn execute_command(
        &mut self,
        cmd: Command,
        conn: &Connection,
        project: &str,
    ) -> Result<(), String> {
        match cmd {
            Command::CreateTask { title, branch } => {
                let default_column = self
                    .config
                    .columns
                    .first()
                    .map(|c| c.name.clone())
                    .unwrap_or_default();
                let new_task = Task::new(&title, &branch, &default_column);
                task::validate_task(&new_task).map_err(|e| e.to_string())?;
                db::create_task(conn, &new_task).map_err(|e| e.to_string())?;
                self.tasks.push(new_task);
                info!(title = %title, branch = %branch, "Task created");
                Ok(())
            }
            Command::DeleteTask { id } => {
                db::delete_task(conn, &id).map_err(|e| e.to_string())?;
                self.tasks.retain(|t| t.id != id);
                info!(id = %id, "Task deleted");
                Ok(())
            }
            Command::MoveTask { id, to_column } => {
                if let Some(task) = self.tasks.iter_mut().find(|t| t.id == id) {
                    task.column = to_column.clone();
                    task.updated_at = chrono::Utc::now();
                    db::update_task(conn, task).map_err(|e| e.to_string())?;
                    info!(id = %id, column = %to_column, "Task moved");
                }
                Ok(())
            }
            Command::AttachTmux { id } => {
                if let Some(task) = self.tasks.iter().find(|t| t.id == id) {
                    let session_name = format!("ait-{}-{}", project, task.branch);
                    tmux::create_session(project, task).map_err(|e| e.to_string())?;
                    tmux::attach_session(&session_name).map_err(|e| e.to_string())?;
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
                    info!(id = %id, field = %field, "Task edited");
                }
                Ok(())
            }
            Command::MoveSelectionDown => {
                let column_name = self
                    .config
                    .columns
                    .get(self.selected_column)
                    .map(|c| c.name.clone());
                if let Some(col) = column_name {
                    let col_tasks = self.tasks_in_column(&col);
                    if let Some(sel) = self.selected_task {
                        if sel < col_tasks.len() - 1 {
                            self.selected_task = Some(sel + 1);
                        }
                    } else if !col_tasks.is_empty() {
                        self.selected_task = Some(0);
                    }
                }
                Ok(())
            }
            Command::MoveSelectionUp => {
                if let Some(sel) = self.selected_task {
                    if sel > 0 {
                        self.selected_task = Some(sel - 1);
                    }
                }
                Ok(())
            }
            Command::SelectTask => Ok(()),
            Command::StartTask => {
                if let Some(task_id) = self.get_selected_task_id() {
                    if let Some(task) = self.tasks.iter_mut().find(|t| t.id == task_id) {
                        task.status = Status::InProgress;
                        task.updated_at = chrono::Utc::now();
                        db::update_task(conn, task).map_err(|e| e.to_string())?;
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
                        info!(id = %task_id, "Task completed");
                    }
                }
                Ok(())
            }
            Command::ToggleCreateDialog => {
                self.show_create_dialog = !self.show_create_dialog;
                if !self.show_create_dialog {
                    self.create_title.clear();
                    self.create_branch.clear();
                }
                Ok(())
            }
            Command::ConfirmCreate => {
                if !self.create_title.is_empty() && !self.create_branch.is_empty() {
                    return self.execute_command(
                        Command::CreateTask {
                            title: self.create_title.clone(),
                            branch: self.create_branch.clone(),
                        },
                        conn,
                        project,
                    );
                }
                self.show_create_dialog = false;
                self.create_title.clear();
                self.create_branch.clear();
                Ok(())
            }
            Command::CancelCreate => {
                self.show_create_dialog = false;
                self.create_title.clear();
                self.create_branch.clear();
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

fn centered_rect(area: Rect, width: u16, height: u16) -> Rect {
    Rect {
        x: area.x + (area.width.saturating_sub(width)) / 2,
        y: area.y + (area.height.saturating_sub(height)) / 2,
        width,
        height,
    }
}

fn render_ui(f: &mut Frame<'_>, state: &AppState) {
    let areas = Layout::default()
        .direction(Direction::Vertical)
        .constraints([Constraint::Length(3), Constraint::Min(0)].as_ref())
        .split(f.area());

    let header_area = areas[0];
    let body_area = areas[1];

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
        let dialog_area = centered_rect(body_area, 40, 10);
        let dialog_block = Block::default()
            .title("Create Task")
            .borders(Borders::ALL)
            .style(Style::default().bg(Color::DarkGray));

        f.render_widget(Clear, body_area);
        f.render_widget(dialog_block, dialog_area);

        let content = Text::raw(format!(
            "Title: {}\nBranch: {}",
            state.create_title, state.create_branch
        ));
        f.render_widget(
            Paragraph::new(content),
            dialog_area.inner(Margin {
                horizontal: 1,
                vertical: 1,
            }),
        );
    } else {
        let column_areas = Layout::default()
            .direction(Direction::Horizontal)
            .constraints(
                state
                    .config
                    .columns
                    .iter()
                    .map(|_| Constraint::Percentage(100 / state.config.columns.len() as u16))
                    .collect::<Vec<_>>(),
            )
            .split(body_area);

        for (i, column) in state.config.columns.iter().enumerate() {
            let column_area = column_areas[i];
            let col_name = &column.name;
            let col_tasks: Vec<&Task> = state
                .tasks
                .iter()
                .filter(|t| t.column == *col_name)
                .collect();

            let column_block = Block::default()
                .title(format!(" {} ({}) ", column.name, col_tasks.len()))
                .borders(Borders::ALL)
                .style(if i == state.selected_column {
                    Style::default().bg(Color::Blue).fg(Color::White)
                } else {
                    Style::default()
                });

            f.render_widget(column_block, column_area);

            let task_list = col_tasks
                .iter()
                .enumerate()
                .map(|(idx, task)| {
                    let is_selected =
                        state.selected_column == i && state.selected_task == Some(idx);
                    let prefix = if is_selected { ">> " } else { "   " };
                    let status_str = match task.status {
                        Status::Open => "[ ]",
                        Status::InProgress => "[#]",
                        Status::Blocked => "[!]",
                        Status::Done => "[x]",
                    };
                    format!("{}{} {}", prefix, status_str, task.title)
                })
                .collect::<Vec<_>>()
                .join("\n");

            let task_widget = Paragraph::new(Text::raw(task_list));
            f.render_widget(
                task_widget,
                column_area.inner(Margin {
                    horizontal: 1,
                    vertical: 1,
                }),
            );
        }
    }

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
    let mut state = AppState::new(config);
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

            let cmd = match key.code {
                KeyCode::Char('q') => break,
                KeyCode::Char('n') => Some(Command::ToggleCreateDialog),
                KeyCode::Char('j') => Some(Command::MoveSelectionDown),
                KeyCode::Char('k') => Some(Command::MoveSelectionUp),
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
                KeyCode::Left => {
                    if state.selected_column > 0 {
                        state.selected_column -= 1;
                        state.selected_task = None;
                    }
                    None
                }
                KeyCode::Right => {
                    if state.selected_column < state.config.columns.len() - 1 {
                        state.selected_column += 1;
                        state.selected_task = None;
                    }
                    None
                }
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
